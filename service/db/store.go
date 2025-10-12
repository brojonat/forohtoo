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
	Limit         int32
	Offset        int32
}

// ListTransactionsByWalletAndTimeRangeParams contains time range query parameters.
type ListTransactionsByWalletAndTimeRangeParams struct {
	WalletAddress string
	StartTime     time.Time
	EndTime       time.Time
}

// CreateTransaction inserts a new transaction into the database.
func (s *Store) CreateTransaction(ctx context.Context, params CreateTransactionParams) (*Transaction, error) {
	// Convert domain params to sqlc params
	sqlcParams := dbgen.CreateTransactionParams{
		Signature:          params.Signature,
		WalletAddress:      params.WalletAddress,
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

// GetTransaction retrieves a transaction by its signature.
func (s *Store) GetTransaction(ctx context.Context, signature string) (*Transaction, error) {
	result, err := s.q.GetTransaction(ctx, signature)
	if err != nil {
		return nil, err
	}

	return dbTransactionToDomain(&result), nil
}

// ListTransactionsByWallet retrieves transactions for a wallet with pagination.
func (s *Store) ListTransactionsByWallet(ctx context.Context, params ListTransactionsByWalletParams) ([]*Transaction, error) {
	sqlcParams := dbgen.ListTransactionsByWalletParams{
		WalletAddress: params.WalletAddress,
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
func (s *Store) CountTransactionsByWallet(ctx context.Context, walletAddress string) (int64, error) {
	return s.q.CountTransactionsByWallet(ctx, walletAddress)
}

// GetLatestTransactionByWallet retrieves the most recent transaction for a wallet.
func (s *Store) GetLatestTransactionByWallet(ctx context.Context, walletAddress string) (*Transaction, error) {
	result, err := s.q.GetLatestTransactionByWallet(ctx, walletAddress)
	if err != nil {
		return nil, err
	}

	return dbTransactionToDomain(&result), nil
}

// GetTransactionsSince retrieves transactions for a wallet since a given time.
func (s *Store) GetTransactionsSince(ctx context.Context, walletAddress string, since time.Time) ([]*Transaction, error) {
	params := dbgen.GetTransactionsSinceParams{
		WalletAddress: walletAddress,
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

// Wallet represents a registered wallet that the server monitors.
type Wallet struct {
	Address      string
	PollInterval time.Duration
	LastPollTime *time.Time
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CreateWalletParams contains the parameters for registering a wallet.
type CreateWalletParams struct {
	Address      string
	PollInterval time.Duration
	Status       string
}

// CreateWallet registers a new wallet for monitoring.
func (s *Store) CreateWallet(ctx context.Context, params CreateWalletParams) (*Wallet, error) {
	sqlcParams := dbgen.CreateWalletParams{
		Address:      params.Address,
		PollInterval: pgIntervalFromDuration(params.PollInterval),
		Status:       params.Status,
	}

	result, err := s.q.CreateWallet(ctx, sqlcParams)
	if err != nil {
		return nil, err
	}

	return dbWalletToDomain(&result), nil
}

// GetWallet retrieves a wallet by its address.
func (s *Store) GetWallet(ctx context.Context, address string) (*Wallet, error) {
	result, err := s.q.GetWallet(ctx, address)
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

// UpdateWalletPollTime updates the last poll time for a wallet.
func (s *Store) UpdateWalletPollTime(ctx context.Context, address string, pollTime time.Time) (*Wallet, error) {
	params := dbgen.UpdateWalletPollTimeParams{
		Address:      address,
		LastPollTime: pgtype.Timestamptz{Time: pollTime, Valid: true},
	}

	result, err := s.q.UpdateWalletPollTime(ctx, params)
	if err != nil {
		return nil, err
	}

	return dbWalletToDomain(&result), nil
}

// UpdateWalletStatus updates the status of a wallet.
func (s *Store) UpdateWalletStatus(ctx context.Context, address string, status string) (*Wallet, error) {
	params := dbgen.UpdateWalletStatusParams{
		Address: address,
		Status:  status,
	}

	result, err := s.q.UpdateWalletStatus(ctx, params)
	if err != nil {
		return nil, err
	}

	return dbWalletToDomain(&result), nil
}

// DeleteWallet removes a wallet from monitoring.
func (s *Store) DeleteWallet(ctx context.Context, address string) error {
	return s.q.DeleteWallet(ctx, address)
}

// WalletExists checks if a wallet is registered.
func (s *Store) WalletExists(ctx context.Context, address string) (bool, error) {
	return s.q.WalletExists(ctx, address)
}

// Helper functions to convert between sqlc types and domain types

func dbTransactionToDomain(db *dbgen.Transaction) *Transaction {
	return &Transaction{
		Signature:          db.Signature,
		WalletAddress:      db.WalletAddress,
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
		Address:      db.Address,
		PollInterval: durationFromPgInterval(db.PollInterval),
		LastPollTime: timePtrFromPgTimestamptz(db.LastPollTime),
		Status:       db.Status,
		CreatedAt:    db.CreatedAt.Time,
		UpdatedAt:    db.UpdatedAt.Time,
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
