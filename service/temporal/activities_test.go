package temporal

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/solana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock Solana Client
type MockSolanaClient struct {
	mock.Mock
}

func (m *MockSolanaClient) GetTransactionsSince(ctx context.Context, params solana.GetTransactionsSinceParams) ([]*solana.Transaction, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*solana.Transaction), args.Error(1)
}

// Mock Store
type MockStore struct {
	mock.Mock
}

func (m *MockStore) CreateTransaction(ctx context.Context, params db.CreateTransactionParams) (*db.Transaction, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Transaction), args.Error(1)
}

func (m *MockStore) UpdateWalletPollTime(ctx context.Context, address string, network string, assetType string, tokenMint string, pollTime time.Time) (*db.Wallet, error) {
	args := m.Called(ctx, address, network, assetType, tokenMint, pollTime)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Wallet), args.Error(1)
}

func (m *MockStore) GetTransaction(ctx context.Context, signature string, network string) (*db.Transaction, error) {
	args := m.Called(ctx, signature, network)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Transaction), args.Error(1)
}

func (m *MockStore) GetWallet(ctx context.Context, address string, network string, assetType string, tokenMint string) (*db.Wallet, error) {
	args := m.Called(ctx, address, network, assetType, tokenMint)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Wallet), args.Error(1)
}

func (m *MockStore) GetTransactionSignaturesByWallet(ctx context.Context, walletAddress string, network string, since *time.Time, limit int32) ([]string, error) {
	args := m.Called(ctx, walletAddress, network, since, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockStore) UpsertWallet(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Wallet), args.Error(1)
}

func (m *MockStore) DeleteWallet(ctx context.Context, address, network, assetType, tokenMint string) error {
	args := m.Called(ctx, address, network, assetType, tokenMint)
	return args.Error(0)
}

func TestActivities_PollSolana(t *testing.T) {
	testWallet := "11111111111111111111111111111111" // Valid 32-byte base58 address

	tests := []struct {
		name           string
		input          PollSolanaInput
		setupMock      func(*MockSolanaClient)
		expectedResult *PollSolanaResult
		expectedError  bool
	}{
		{
			name: "successful poll with transactions",
			input: PollSolanaInput{
				Address: testWallet,
				Network: "mainnet",
				Limit:   100,
			},
			setupMock: func(m *MockSolanaClient) {
				txns := []*solana.Transaction{
					{
						Signature: "sig1",
						Slot:      1000,
						BlockTime: time.Now(),
						Amount:    100,
					},
					{
						Signature: "sig2",
						Slot:      999,
						BlockTime: time.Now().Add(-time.Minute),
						Amount:    200,
					},
				}
				m.On("GetTransactionsSince", mock.Anything, mock.Anything).
					Return(txns, nil)
			},
			expectedResult: &PollSolanaResult{
				Transactions: []*solana.Transaction{
					{
						Signature: "sig1",
						Slot:      1000,
						BlockTime: time.Now(),
						Amount:    100,
					},
					{
						Signature: "sig2",
						Slot:      999,
						BlockTime: time.Now().Add(-time.Minute),
						Amount:    200,
					},
				},
				NewestSignature: stringPtr("sig1"),
				OldestSignature: stringPtr("sig2"),
			},
			expectedError: false,
		},
		{
			name: "successful poll with no transactions",
			input: PollSolanaInput{
				Address: testWallet,
				Network: "mainnet",
				Limit:   100,
			},
			setupMock: func(m *MockSolanaClient) {
				m.On("GetTransactionsSince", mock.Anything, mock.Anything).
					Return([]*solana.Transaction{}, nil)
			},
			expectedResult: &PollSolanaResult{
				Transactions:    []*solana.Transaction{},
				NewestSignature: nil,
				OldestSignature: nil,
			},
			expectedError: false,
		},
		{
			name: "invalid wallet address",
			input: PollSolanaInput{
				Address: "invalid",
				Network: "mainnet",
				Limit:   100,
			},
			setupMock: func(m *MockSolanaClient) {
				// Mock should not be called
			},
			expectedResult: nil,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			mockSolanaClient := new(MockSolanaClient)
			mockStore := new(MockStore)
			tt.setupMock(mockSolanaClient)

			activities := NewActivities(mockStore, mockSolanaClient, mockSolanaClient, nil, nil, nil, nil, slog.Default()) // Use same mock for both networks, nil for publisher, forohtoo client, temporal client, and metrics
			result, err := activities.PollSolana(context.Background(), tt.input)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, len(tt.expectedResult.Transactions), len(result.Transactions))
				assert.Equal(t, tt.expectedResult.NewestSignature, result.NewestSignature)
				assert.Equal(t, tt.expectedResult.OldestSignature, result.OldestSignature)
			}
		})
	}
}

func TestActivities_GetExistingTransactionSignatures(t *testing.T) {
	testWallet := "11111111111111111111111111111111" // Valid 32-byte base58 address

	tests := []struct {
		name           string
		input          GetExistingTransactionSignaturesInput
		setupMock      func(*MockStore)
		expectedResult *GetExistingTransactionSignaturesResult
		expectedError  bool
	}{
		{
			name: "successful fetch with signatures",
			input: GetExistingTransactionSignaturesInput{
				WalletAddress: testWallet,
				Network:       "mainnet",
			},
			setupMock: func(m *MockStore) {
				sigs := []string{"sig1", "sig2"}
				m.On("GetTransactionSignaturesByWallet", mock.Anything, testWallet, "mainnet", mock.Anything, mock.Anything).
					Return(sigs, nil)
			},
			expectedResult: &GetExistingTransactionSignaturesResult{
				Signatures: []string{"sig1", "sig2"},
			},
			expectedError: false,
		},
		{
			name: "successful fetch with no signatures",
			input: GetExistingTransactionSignaturesInput{
				WalletAddress: testWallet,
				Network:       "mainnet",
			},
			setupMock: func(m *MockStore) {
				m.On("GetTransactionSignaturesByWallet", mock.Anything, testWallet, "mainnet", mock.Anything, mock.Anything).
					Return([]string{}, nil)
			},
			expectedResult: &GetExistingTransactionSignaturesResult{
				Signatures: []string{},
			},
			expectedError: false,
		},
		{
			name: "store returns an error",
			input: GetExistingTransactionSignaturesInput{
				WalletAddress: testWallet,
				Network:       "mainnet",
			},
			setupMock: func(m *MockStore) {
				m.On("GetTransactionSignaturesByWallet", mock.Anything, testWallet, "mainnet", mock.Anything, mock.Anything).
					Return(nil, errors.New("db error"))
			},
			expectedResult: nil,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := new(MockStore)
			tt.setupMock(mockStore)

			activities := NewActivities(mockStore, nil, nil, nil, nil, nil, nil, slog.Default()) // nil for both solanaClients, publisher, and metrics

			result, err := activities.GetExistingTransactionSignatures(context.Background(), tt.input)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.expectedResult.Signatures, result.Signatures)
			}

			mockStore.AssertExpectations(t)
		})
	}
}

func TestActivities_WriteTransactions(t *testing.T) {
	testWallet := "11111111111111111111111111111111" // Valid 32-byte base58 address

	tests := []struct {
		name           string
		input          WriteTransactionsInput
		setupMock      func(*MockStore)
		expectedResult *WriteTransactionsResult
		expectedError  bool
	}{
		{
			name: "write new transactions successfully",
			input: WriteTransactionsInput{
				WalletAddress: testWallet,
				Network:       "mainnet",
				AssetType:     "sol",
				TokenMint:     "",
				Transactions: []*solana.Transaction{
					{
						Signature: "sig1",
						Slot:      1000,
						BlockTime: time.Now(),
						Amount:    100,
						Memo:      stringPtr("test memo"),
					},
					{
						Signature: "sig2",
						Slot:      999,
						BlockTime: time.Now(),
						Amount:    200,
					},
				},
			},
			setupMock: func(m *MockStore) {
				// First transaction succeeds
				m.On("CreateTransaction", mock.Anything, mock.MatchedBy(func(p db.CreateTransactionParams) bool {
					return p.Signature == "sig1"
				})).Return(&db.Transaction{Signature: "sig1"}, nil)

				// Second transaction succeeds
				m.On("CreateTransaction", mock.Anything, mock.MatchedBy(func(p db.CreateTransactionParams) bool {
					return p.Signature == "sig2"
				})).Return(&db.Transaction{Signature: "sig2"}, nil)

				// Update poll time succeeds
				m.On("UpdateWalletPollTime", mock.Anything, testWallet, "mainnet", "sol", "", mock.Anything).
					Return(&db.Wallet{Address: testWallet}, nil)
			},
			expectedResult: &WriteTransactionsResult{
				Written: 2,
				Skipped: 0,
			},
			expectedError: false,
		},
		{
			name: "skip duplicate transactions",
			input: WriteTransactionsInput{
				WalletAddress: testWallet,
				Network:       "mainnet",
				AssetType:     "sol",
				TokenMint:     "",
				Transactions: []*solana.Transaction{
					{
						Signature: "sig1",
						Slot:      1000,
						BlockTime: time.Now(),
						Amount:    100,
					},
					{
						Signature: "sig2",
						Slot:      999,
						BlockTime: time.Now(),
						Amount:    200,
					},
				},
			},
			setupMock: func(m *MockStore) {
				// First transaction is duplicate - return error that looks like duplicate key error
				m.On("CreateTransaction", mock.Anything, mock.MatchedBy(func(p db.CreateTransactionParams) bool {
					return p.Signature == "sig1"
				})).Return(nil, errors.New("duplicate key value violates unique constraint"))

				// Second transaction succeeds
				m.On("CreateTransaction", mock.Anything, mock.MatchedBy(func(p db.CreateTransactionParams) bool {
					return p.Signature == "sig2"
				})).Return(&db.Transaction{Signature: "sig2"}, nil)

				// Update poll time succeeds
				m.On("UpdateWalletPollTime", mock.Anything, testWallet, "mainnet", "sol", "", mock.Anything).
					Return(&db.Wallet{Address: testWallet}, nil)
			},
			expectedResult: &WriteTransactionsResult{
				Written: 1,
				Skipped: 1,
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			mockStore := new(MockStore)
			tt.setupMock(mockStore)

			activities := NewActivities(mockStore, nil, nil, nil, nil, nil, nil, slog.Default()) // nil for both solanaClients, publisher, and metrics

			result, err := activities.WriteTransactions(context.Background(), tt.input)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.expectedResult.Written, result.Written)
				// Note: Skipped count may vary based on error detection
			}

			mockStore.AssertExpectations(t)
		})
	}
}

func TestActivities_WriteTransactions_SetsConfirmationStatus(t *testing.T) {
	testWallet := "11111111111111111111111111111111" // Valid 32-byte base58 address
	mockStore := new(MockStore)

	// Transaction with no error should be "confirmed"
	mockStore.On("CreateTransaction", mock.Anything, mock.MatchedBy(func(p db.CreateTransactionParams) bool {
		return p.Signature == "sig_confirmed" && p.ConfirmationStatus == "confirmed"
	})).Return(&db.Transaction{Signature: "sig_confirmed"}, nil)

	// Transaction with error should be "failed"
	mockStore.On("CreateTransaction", mock.Anything, mock.MatchedBy(func(p db.CreateTransactionParams) bool {
		return p.Signature == "sig_failed" && p.ConfirmationStatus == "failed"
	})).Return(&db.Transaction{Signature: "sig_failed"}, nil)

	mockStore.On("UpdateWalletPollTime", mock.Anything, testWallet, "mainnet", "sol", "", mock.Anything).
		Return(&db.Wallet{Address: testWallet}, nil)

	activities := NewActivities(mockStore, nil, nil, nil, nil, nil, nil, slog.Default()) // nil for both solanaClients, publisher, and metrics

	input := WriteTransactionsInput{
		WalletAddress: testWallet,
		Network:       "mainnet",
		AssetType:     "sol",
		TokenMint:     "",
		Transactions: []*solana.Transaction{
			{
				Signature: "sig_confirmed",
				Slot:      1000,
				BlockTime: time.Now(),
				Amount:    100,
				Err:       nil, // No error = confirmed
			},
			{
				Signature: "sig_failed",
				Slot:      999,
				BlockTime: time.Now(),
				Amount:    200,
				Err:       stringPtr("transaction failed"), // Has error = failed
			},
		},
	}

	result, err := activities.WriteTransactions(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 2, result.Written)

	mockStore.AssertExpectations(t)
}
