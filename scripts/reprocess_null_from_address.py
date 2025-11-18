#!/usr/bin/env -S uv run --quiet --script
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "psycopg2-binary",
#     "solana",
#     "solders",
# ]
# ///
"""
Reprocess transactions with NULL from_address.

This script fetches transactions from the database that have NULL from_address,
retrieves the full transaction details from Solana RPC, re-parses them to extract
the from_address, and updates the database.

Usage:
    ./scripts/reprocess_null_from_address.py --wallet CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv --network mainnet
"""

import argparse
import os
import sys
import time
from typing import Optional

import psycopg2
from solana.rpc.api import Client as SolanaClient
from solders.pubkey import Pubkey
from solders.signature import Signature


def parse_token_transfer_authority(instruction_data: bytes, account_keys: list, instruction_accounts: list) -> Optional[str]:
    """
    Extract the authority (from_address) from a Token Transfer instruction.

    Token Transfer instruction (type 3):
    - Account layout: [source_token_account, destination_token_account, authority]
    - Authority is at index 2
    """
    if len(instruction_data) < 9:
        return None

    instruction_type = instruction_data[0]

    # Transfer (type 3)
    if instruction_type == 3:
        if len(instruction_accounts) >= 3:
            authority_index = instruction_accounts[2]
            if authority_index < len(account_keys):
                return str(account_keys[authority_index])

    # TransferChecked (type 12)
    elif instruction_type == 12:
        if len(instruction_accounts) >= 4:
            authority_index = instruction_accounts[3]
            if authority_index < len(account_keys):
                return str(account_keys[authority_index])

    return None


def extract_from_address_from_transaction(rpc_client: SolanaClient, signature: str) -> Optional[str]:
    """
    Fetch a transaction from Solana RPC and extract the from_address.
    """
    try:
        # Fetch transaction with base64 encoding to get raw instruction data
        sig = Signature.from_string(signature)
        result = rpc_client.get_transaction(
            sig,
            encoding="base64",
            max_supported_transaction_version=0
        )

        if not result.value:
            print(f"  ⚠️  Transaction not found on RPC (may be pruned): {signature[:20]}...")
            return None

        # Parse transaction
        tx = result.value.transaction
        if not tx or not tx.transaction or not tx.transaction.message:
            print(f"  ⚠️  Invalid transaction structure: {signature[:20]}...")
            return None

        message = tx.transaction.message
        account_keys = message.account_keys

        # Token Program IDs
        TOKEN_PROGRAM_ID = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
        TOKEN_2022_PROGRAM_ID = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"

        # Look through instructions for token transfers
        for instruction in message.instructions:
            try:
                # Check if this is a ParsedInstruction (Solana RPC sometimes returns these for token programs)
                if hasattr(instruction, 'parsed'):
                    # This is a parsed instruction - extract from_address from parsed data
                    parsed = instruction.parsed
                    if isinstance(parsed, dict):
                        # For token transfer instructions, the authority is in the "info" section
                        info = parsed.get('info', {})
                        # Try different possible keys for the authority/signer
                        authority = info.get('authority') or info.get('owner') or info.get('multisigAuthority')
                        if authority:
                            return authority
                    continue

                # This is a raw instruction
                program_id_index = instruction.program_id_index
                program_id = str(account_keys[program_id_index])

                # Check if this is a token program instruction
                if program_id in [TOKEN_PROGRAM_ID, TOKEN_2022_PROGRAM_ID]:
                    # Get instruction data and accounts
                    instruction_data = bytes(instruction.data)
                    instruction_accounts = instruction.accounts

                    from_addr = parse_token_transfer_authority(
                        instruction_data,
                        account_keys,
                        instruction_accounts
                    )

                    if from_addr:
                        return from_addr
            except Exception as e:
                print(f"  ⚠️  Error parsing instruction: {e}")
                continue

        print(f"  ⚠️  No token transfer found in transaction: {signature[:20]}...")
        return None

    except Exception as e:
        print(f"  ❌ Error fetching transaction {signature[:20]}...: {e}")
        return None


def reprocess_transactions(database_url: str, rpc_url: str, wallet_address: str, network: str, limit: int = 1000):
    """
    Reprocess transactions with NULL from_address.
    """
    print(f"\n🔍 Reprocessing transactions for wallet: {wallet_address}")
    print(f"   Network: {network}")
    print(f"   RPC: {rpc_url}")
    print(f"   Database: {database_url.split('@')[1] if '@' in database_url else 'localhost'}\n")

    # Connect to database
    conn = psycopg2.connect(database_url)
    cursor = conn.cursor()

    # Connect to Solana RPC
    rpc_client = SolanaClient(rpc_url)

    try:
        # Fetch transactions with NULL from_address
        print(f"📊 Fetching transactions with NULL from_address...")
        cursor.execute("""
            SELECT signature, block_time
            FROM transactions
            WHERE wallet_address = %s
              AND network = %s
              AND from_address IS NULL
            ORDER BY block_time DESC
            LIMIT %s
        """, (wallet_address, network, limit))

        transactions = cursor.fetchall()
        total = len(transactions)

        if total == 0:
            print("✅ No transactions with NULL from_address found!")
            return

        print(f"📝 Found {total} transactions with NULL from_address\n")

        # Process each transaction
        updated = 0
        failed = 0
        not_found = 0

        for i, (signature, block_time) in enumerate(transactions, 1):
            print(f"[{i}/{total}] Processing {signature[:20]}... ({block_time})")

            # Extract from_address from Solana RPC
            from_address = extract_from_address_from_transaction(rpc_client, signature)

            if from_address:
                # Update database
                cursor.execute("""
                    UPDATE transactions
                    SET from_address = %s
                    WHERE signature = %s AND network = %s
                """, (from_address, signature, network))

                conn.commit()
                print(f"  ✅ Updated: from_address = {from_address}")
                updated += 1
            else:
                not_found += 1

            # Rate limiting (public RPC is ~2 RPS)
            if i < total:
                time.sleep(0.6)  # ~1.5 RPS to be safe

        print(f"\n{'='*60}")
        print(f"📊 Summary:")
        print(f"   Total processed: {total}")
        print(f"   ✅ Updated: {updated}")
        print(f"   ⚠️  Not found: {not_found}")
        print(f"   ❌ Failed: {failed}")
        print(f"{'='*60}\n")

    finally:
        cursor.close()
        conn.close()


def main():
    parser = argparse.ArgumentParser(description="Reprocess transactions with NULL from_address")
    parser.add_argument("--wallet", required=True, help="Wallet address to process")
    parser.add_argument("--network", required=True, choices=["mainnet", "devnet"], help="Network")
    parser.add_argument("--limit", type=int, default=1000, help="Maximum transactions to process")
    parser.add_argument("--database-url", help="PostgreSQL connection string (or set DATABASE_URL env var)")
    parser.add_argument("--rpc-url", help="Solana RPC URL (or set SOLANA_RPC_URL env var)")

    args = parser.parse_args()

    # Get database URL
    database_url = args.database_url or os.getenv("DATABASE_URL")
    if not database_url:
        print("❌ Error: DATABASE_URL not set. Use --database-url or set DATABASE_URL environment variable.")
        sys.exit(1)

    # Get RPC URL
    if args.network == "mainnet":
        default_rpc = "https://api.mainnet-beta.solana.com"
        env_var = "SOLANA_MAINNET_RPC_URL"
    else:
        default_rpc = "https://api.devnet.solana.com"
        env_var = "SOLANA_DEVNET_RPC_URL"

    rpc_url = args.rpc_url or os.getenv(env_var) or default_rpc

    # Run reprocessing
    reprocess_transactions(
        database_url=database_url,
        rpc_url=rpc_url,
        wallet_address=args.wallet,
        network=args.network,
        limit=args.limit
    )

    print("✅ Done!\n")


if __name__ == "__main__":
    main()
