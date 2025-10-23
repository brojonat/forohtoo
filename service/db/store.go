package db

import (
	"context"
	"time"

	"github.com/brojonat/forohtoo/service/db/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store provides database operations for the service.
// It wraps the generated sqlc Querier interface with a concrete implementation.
type Store struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

// NewStore creates a new Store with the given database connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		pool: pool,
		q:    dbgen.New(pool),
	}
}

// Transaction represents a Solana transaction in our system.
// This is a domain model that wraps the generated database model.
type Transaction struct {
	Signature          string
	WalletAddress      string
	Network            string  // "mainnet" or "devnet"
	Slot               int64
	BlockTime          time.Time
	Amount             int64
	TokenMint          *string // nil for native SOL
	Memo               *string
	ConfirmationStatus string
	CreatedAt          time.Time
	FromAddress        *string // source wallet (sender)
}

// CreateTransactionParams contains the parameters for creating a transaction.
type CreateTransactionParams struct {
	Signature          string
	WalletAddress      string
	Network            string
	Slot               int64
	BlockTime          time.Time
	Amount             int64
	TokenMint          *string
	Memo               *string
	ConfirmationStatus string
	FromAddress        *string
}

// ListTransactionsByWalletParams contains pagination parameters.
type ListTransactionsByWalletParams struct {
	WalletAddress string
	Network       string
	Limit         int32
	Offset        int32
}

// ListTransactionsByWalletAndTimeRangeParams contains time range query parameters.
type ListTransactionsByWalletAndTimeRangeParams struct {
	WalletAddress string
	Network       string
	StartTime     time.Time
	EndTime       time.Time
}

// CreateTransaction inserts a new transaction into the database.
func (s *Store) CreateTransaction(ctx context.Context, params CreateTransactionParams) (*Transaction, error) {
	// Convert domain params to sqlc params
	sqlcParams := dbgen.CreateTransactionParams{
		Signature:          params.Signature,
		WalletAddress:      params.WalletAddress,
		Network:            params.Network,
		Slot:               params.Slot,
		BlockTime:          pgtype.Timestamptz{Time: params.BlockTime, Valid: true},
		Amount:             params.Amount,
		TokenMint:          pgtextFromStringPtr(params.TokenMint),
		Memo:               pgtextFromStringPtr(params.Memo),
		ConfirmationStatus: params.ConfirmationStatus,
		FromAddress:        pgtextFromStringPtr(params.FromAddress),
	}

	result, err := s.q.CreateTransaction(ctx, sqlcParams)
	if err != nil {
		return nil, err
	}

	return dbTransactionToDomain(&result), nil
}

// GetTransaction retrieves a transaction by its signature and network.
func (s *Store) GetTransaction(ctx context.Context, signature string, network string) (*Transaction, error) {
	params := dbgen.GetTransactionParams{
		Signature: signature,
		Network:   network,
	}
	result, err := s.q.GetTransaction(ctx, params)
	if err != nil {
		return nil, err
	}

	return dbTransactionToDomain(&result), nil
}

// GetTransactionSignaturesByWallet retrieves transaction signatures for a wallet.
// Limit controls the maximum number of signatures returned (ordered by most recent first).
func (s *Store) GetTransactionSignaturesByWallet(ctx context.Context, walletAddress string, network string, since *time.Time, limit int32) ([]string, error) {
	var sinceVal pgtype.Timestamptz
	if since != nil {
		sinceVal = pgtype.Timestamptz{Time: *since, Valid: true}
	}
	arg := dbgen.GetTransactionSignaturesByWalletParams{
		WalletAddress: walletAddress,
		Network:       network,
		Since:         sinceVal,
		LimitCount:    limit,
	}
	return s.q.GetTransactionSignaturesByWallet(ctx, arg)
}

// ListTransactionsByWallet retrieves transactions for a wallet with pagination.
func (s *Store) ListTransactionsByWallet(ctx context.Context, params ListTransactionsByWalletParams) ([]*Transaction, error) {
	sqlcParams := dbgen.ListTransactionsByWalletParams{
		WalletAddress: params.WalletAddress,
		Network:       params.Network,
		Limit:         params.Limit,
		Offset:        params.Offset,
	}

	results, err := s.q.ListTransactionsByWallet(ctx, sqlcParams)
	if err != nil {
		return nil, err
	}

	transactions := make([]*Transaction, len(results))
	for i, result := range results {
		transactions[i] = dbTransactionToDomain(&result)
	}

	return transactions, nil
}

// ListTransactionsByWalletAndTimeRange retrieves transactions for a wallet within a time range.
func (s *Store) ListTransactionsByWalletAndTimeRange(ctx context.Context, params ListTransactionsByWalletAndTimeRangeParams) ([]*Transaction, error) {
	sqlcParams := dbgen.ListTransactionsByWalletAndTimeRangeParams{
		WalletAddress: params.WalletAddress,
		Network:       params.Network,
		BlockTime:     pgtype.Timestamptz{Time: params.StartTime, Valid: true},
		BlockTime_2:   pgtype.Timestamptz{Time: params.EndTime, Valid: true},
	}

	results, err := s.q.ListTransactionsByWalletAndTimeRange(ctx, sqlcParams)
	if err != nil {
		return nil, err
	}

	transactions := make([]*Transaction, len(results))
	for i, result := range results {
		transactions[i] = dbTransactionToDomain(&result)
	}

	return transactions, nil
}

// CountTransactionsByWallet counts transactions for a wallet.
func (s *Store) CountTransactionsByWallet(ctx context.Context, walletAddress string, network string) (int64, error) {
	params := dbgen.CountTransactionsByWalletParams{
		WalletAddress: walletAddress,
		Network:       network,
	}
	return s.q.CountTransactionsByWallet(ctx, params)
}

// GetLatestTransactionByWallet retrieves the most recent transaction for a wallet.
func (s *Store) GetLatestTransactionByWallet(ctx context.Context, walletAddress string, network string) (*Transaction, error) {
	params := dbgen.GetLatestTransactionByWalletParams{
		WalletAddress: walletAddress,
		Network:       network,
	}
	result, err := s.q.GetLatestTransactionByWallet(ctx, params)
	if err != nil {
		return nil, err
	}

	return dbTransactionToDomain(&result), nil
}

// GetTransactionsSince retrieves transactions for a wallet since a given time.
func (s *Store) GetTransactionsSince(ctx context.Context, walletAddress string, network string, since time.Time) ([]*Transaction, error) {
	params := dbgen.GetTransactionsSinceParams{
		WalletAddress: walletAddress,
		Network:       network,
		BlockTime:     pgtype.Timestamptz{Time: since, Valid: true},
	}

	results, err := s.q.GetTransactionsSince(ctx, params)
	if err != nil {
		return nil, err
	}

	transactions := make([]*Transaction, len(results))
	for i, result := range results {
		transactions[i] = dbTransactionToDomain(&result)
	}

	return transactions, nil
}

// DeleteTransactionsOlderThan deletes transactions older than the given time.
func (s *Store) DeleteTransactionsOlderThan(ctx context.Context, before time.Time) error {
	return s.q.DeleteTransactionsOlderThan(ctx, pgtype.Timestamptz{Time: before, Valid: true})
}

// ListTransactionsByTimeRange retrieves transactions across all wallets in a time range.
func (s *Store) ListTransactionsByTimeRange(ctx context.Context, start time.Time, end time.Time) ([]*Transaction, error) {
	params := dbgen.ListTransactionsByTimeRangeParams{
		StartTime: pgtype.Timestamptz{Time: start, Valid: true},
		EndTime:   pgtype.Timestamptz{Time: end, Valid: true},
	}
	results, err := s.q.ListTransactionsByTimeRange(ctx, params)
	if err != nil {
		return nil, err
	}
	transactions := make([]*Transaction, len(results))
	for i := range results {
		transactions[i] = dbTransactionToDomain(&results[i])
	}
	return transactions, nil
}

// Wallet represents a registered wallet+asset combination that the server monitors.
type Wallet struct {
	Address                string
	Network                string  // "mainnet" or "devnet"
	AssetType              string  // "sol" or "spl-token"
	TokenMint              string  // empty for SOL, mint address for SPL tokens
	AssociatedTokenAddress *string // nil for SOL, ATA for SPL tokens
	PollInterval           time.Duration
	LastPollTime           *time.Time
	Status                 string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// CreateWalletParams contains the parameters for registering a wallet asset.
type CreateWalletParams struct {
	Address                string
	Network                string
	AssetType              string
	TokenMint              string
	AssociatedTokenAddress *string
	PollInterval           time.Duration
	Status                 string
}

// CreateWallet registers a new wallet+asset for monitoring.
func (s *Store) CreateWallet(ctx context.Context, params CreateWalletParams) (*Wallet, error) {
	sqlcParams := dbgen.CreateWalletParams{
		Address:                params.Address,
		Network:                params.Network,
		AssetType:              params.AssetType,
		TokenMint:              params.TokenMint,
		AssociatedTokenAddress: pgtextFromStringPtr(params.AssociatedTokenAddress),
		PollInterval:           pgIntervalFromDuration(params.PollInterval),
		Status:                 params.Status,
	}

	result, err := s.q.CreateWallet(ctx, sqlcParams)
	if err != nil {
		return nil, err
	}

	return dbWalletToDomain(&result), nil
}

// GetWallet retrieves a wallet+asset by its address, network, asset type, and token mint.
func (s *Store) GetWallet(ctx context.Context, address string, network string, assetType string, tokenMint string) (*Wallet, error) {
	params := dbgen.GetWalletParams{
		Address:   address,
		Network:   network,
		AssetType: assetType,
		TokenMint: tokenMint,
	}
	result, err := s.q.GetWallet(ctx, params)
	if err != nil {
		return nil, err
	}

	return dbWalletToDomain(&result), nil
}

// ListWallets retrieves all registered wallets.
func (s *Store) ListWallets(ctx context.Context) ([]*Wallet, error) {
	results, err := s.q.ListWallets(ctx)
	if err != nil {
		return nil, err
	}

	wallets := make([]*Wallet, len(results))
	for i, result := range results {
		wallets[i] = dbWalletToDomain(&result)
	}

	return wallets, nil
}

// ListActiveWallets retrieves all active wallets ordered by last poll time.
func (s *Store) ListActiveWallets(ctx context.Context) ([]*Wallet, error) {
	results, err := s.q.ListActiveWallets(ctx)
	if err != nil {
		return nil, err
	}

	wallets := make([]*Wallet, len(results))
	for i, result := range results {
		wallets[i] = dbWalletToDomain(&result)
	}

	return wallets, nil
}

// UpdateWalletPollTime updates the last poll time for a wallet+asset.
func (s *Store) UpdateWalletPollTime(ctx context.Context, address string, network string, assetType string, tokenMint string, pollTime time.Time) (*Wallet, error) {
	params := dbgen.UpdateWalletPollTimeParams{
		Address:      address,
		Network:      network,
		AssetType:    assetType,
		TokenMint:    tokenMint,
		LastPollTime: pgtype.Timestamptz{Time: pollTime, Valid: true},
	}

	result, err := s.q.UpdateWalletPollTime(ctx, params)
	if err != nil {
		return nil, err
	}

	return dbWalletToDomain(&result), nil
}

// UpdateWalletStatus updates the status of a wallet+asset.
func (s *Store) UpdateWalletStatus(ctx context.Context, address string, network string, assetType string, tokenMint string, status string) (*Wallet, error) {
	params := dbgen.UpdateWalletStatusParams{
		Address:   address,
		Network:   network,
		AssetType: assetType,
		TokenMint: tokenMint,
		Status:    status,
	}

	result, err := s.q.UpdateWalletStatus(ctx, params)
	if err != nil {
		return nil, err
	}

	return dbWalletToDomain(&result), nil
}

// UpdateWalletPollInterval updates the poll interval for a wallet+asset.
func (s *Store) UpdateWalletPollInterval(ctx context.Context, address string, network string, assetType string, tokenMint string, pollInterval time.Duration) (*Wallet, error) {
	params := dbgen.UpdateWalletPollIntervalParams{
		Address:      address,
		Network:      network,
		AssetType:    assetType,
		TokenMint:    tokenMint,
		PollInterval: pgIntervalFromDuration(pollInterval),
	}

	result, err := s.q.UpdateWalletPollInterval(ctx, params)
	if err != nil {
		return nil, err
	}

	return dbWalletToDomain(&result), nil
}

// DeleteWallet removes a wallet+asset from monitoring.
func (s *Store) DeleteWallet(ctx context.Context, address string, network string, assetType string, tokenMint string) error {
	params := dbgen.DeleteWalletParams{
		Address:   address,
		Network:   network,
		AssetType: assetType,
		TokenMint: tokenMint,
	}
	return s.q.DeleteWallet(ctx, params)
}

// WalletExists checks if a wallet+asset is registered.
func (s *Store) WalletExists(ctx context.Context, address string, network string, assetType string, tokenMint string) (bool, error) {
	params := dbgen.WalletExistsParams{
		Address:   address,
		Network:   network,
		AssetType: assetType,
		TokenMint: tokenMint,
	}
	return s.q.WalletExists(ctx, params)
}

// ListWalletsByAddress retrieves all wallet+asset combinations for a given address.
func (s *Store) ListWalletsByAddress(ctx context.Context, address string) ([]*Wallet, error) {
	results, err := s.q.ListWalletsByAddress(ctx, address)
	if err != nil {
		return nil, err
	}

	wallets := make([]*Wallet, len(results))
	for i, result := range results {
		wallets[i] = dbWalletToDomain(&result)
	}

	return wallets, nil
}

// ListWalletAssets retrieves all assets registered for a specific wallet and network.
func (s *Store) ListWalletAssets(ctx context.Context, address string, network string) ([]*Wallet, error) {
	params := dbgen.ListWalletAssetsParams{
		Address: address,
		Network: network,
	}
	results, err := s.q.ListWalletAssets(ctx, params)
	if err != nil {
		return nil, err
	}

	wallets := make([]*Wallet, len(results))
	for i, result := range results {
		wallets[i] = dbWalletToDomain(&result)
	}

	return wallets, nil
}

// Helper functions to convert between sqlc types and domain types

func dbTransactionToDomain(db *dbgen.Transaction) *Transaction {
	return &Transaction{
		Signature:          db.Signature,
		WalletAddress:      db.WalletAddress,
		Network:            db.Network,
		Slot:               db.Slot,
		BlockTime:          db.BlockTime.Time,
		Amount:             db.Amount,
		TokenMint:          stringPtrFromPgtext(db.TokenMint),
		Memo:               stringPtrFromPgtext(db.Memo),
		ConfirmationStatus: db.ConfirmationStatus,
		CreatedAt:          db.CreatedAt.Time,
		FromAddress:        stringPtrFromPgtext(db.FromAddress),
	}
}

func pgtextFromStringPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func stringPtrFromPgtext(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func dbWalletToDomain(db *dbgen.Wallet) *Wallet {
	return &Wallet{
		Address:                db.Address,
		Network:                db.Network,
		AssetType:              db.AssetType,
		TokenMint:              db.TokenMint,
		AssociatedTokenAddress: stringPtrFromPgtext(db.AssociatedTokenAddress),
		PollInterval:           durationFromPgInterval(db.PollInterval),
		LastPollTime:           timePtrFromPgTimestamptz(db.LastPollTime),
		Status:                 db.Status,
		CreatedAt:              db.CreatedAt.Time,
		UpdatedAt:              db.UpdatedAt.Time,
	}
}

func pgIntervalFromDuration(d time.Duration) pgtype.Interval {
	return pgtype.Interval{
		Microseconds: d.Microseconds(),
		Valid:        true,
	}
}

func durationFromPgInterval(i pgtype.Interval) time.Duration {
	if !i.Valid {
		return 0
	}
	return time.Duration(i.Microseconds) * time.Microsecond
}

func timePtrFromPgTimestamptz(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
