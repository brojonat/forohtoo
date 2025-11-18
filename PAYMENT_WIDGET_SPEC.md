# Payment-Gated Calendly Booking Widget

A lightweight HTML+JavaScript widget for embedding on personal websites that gates Calendly bookings behind Solana/USDC payments verified via forohtoo.

## Overview

This widget provides a pay-to-book flow where visitors must complete a cryptocurrency payment before gaining access to your Calendly scheduling link. The widget monitors payments in real-time using forohtoo's SSE (Server-Sent Events) API.

## User Flow

```
┌─────────────────────────────────────────────────────┐
│ 1. Visitor lands on booking page                    │
│    - See booking description & price                │
│    - Calendly widget is hidden/disabled             │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ 2. Payment request generated                        │
│    - Generate unique booking_id (UUID)              │
│    - Display QR code with payment details           │
│    - Show payment instructions                      │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ 3. Widget subscribes to forohtoo SSE                │
│    - Connect to /api/v1/stream/transactions/:wallet │
│    - Listen for transactions matching criteria      │
│    - Show "Waiting for payment..." status           │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ 4. User scans QR code & sends payment               │
│    - Payment memo includes booking_id                │
│    - Transaction hits Solana blockchain             │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ 5. Forohtoo detects transaction                     │
│    - Wallet polling picks up transaction            │
│    - Publishes to NATS internally                   │
│    - Broadcasts via SSE to widget                   │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ 6. Widget validates & unlocks booking               │
│    - Verify amount matches expected price           │
│    - Verify booking_id in memo                      │
│    - Hide QR code, show success message             │
│    - Reveal Calendly booking widget                 │
└─────────────────────────────────────────────────────┘
```

## Technical Architecture

### Components

**1. HTML Container**
- Single `<div>` element that can be embedded anywhere
- Self-contained with inline CSS or companion stylesheet
- No external framework dependencies (vanilla JS)

**2. JavaScript Widget Controller**
- Manages state machine (pending → waiting → paid → booked)
- Generates unique booking IDs
- Handles SSE connection to forohtoo
- Validates incoming transactions
- Controls UI transitions

**3. QR Code Display**
- Generates Solana payment QR code (solana: URI scheme)
- Includes recipient address, amount, and memo with booking_id
- Optional: Copy-to-clipboard button for mobile users

**4. Calendly Integration**
- Embedded Calendly inline widget or popup
- Initially hidden, revealed after payment
- Optional: Pre-fill user info if collected during payment flow

### External Dependencies

**Required:**
- **forohtoo server**: Running and accessible from browser (CORS configured)
- **Wallet registration**: Target wallet must be registered in forohtoo
- **QR code library**: Small library for generating QR codes (e.g., qrcode.js ~5KB)
- **Calendly account**: For booking functionality

**Optional:**
- **Solana Pay**: For enhanced payment UX on mobile wallets
- **UUID library**: Or use crypto.randomUUID() (modern browsers)

## Configuration

The widget should be configurable via data attributes or JavaScript object:

```html
<div id="payment-booking-widget"
     data-forohtoo-server="https://payments.example.com"
     data-wallet-address="YourSolanaWalletAddress"
     data-payment-amount-usdc="10"
     data-payment-network="mainnet"
     data-calendly-url="https://calendly.com/yourname/30min"
     data-booking-title="30 Minute Consultation"
     data-booking-description="One-on-one consultation call">
</div>

<script src="payment-booking-widget.js"></script>
```

**Configuration Options:**

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `forohtoo-server` | URL | Yes | Base URL of forohtoo HTTP server |
| `wallet-address` | string | Yes | Your Solana wallet address for receiving payments |
| `payment-amount-usdc` | number | Yes | Payment amount in USDC (e.g., 10.00) |
| `payment-network` | string | Yes | Solana network: "mainnet" or "devnet" |
| `calendly-url` | URL | Yes | Your Calendly scheduling link |
| `booking-title` | string | No | Title displayed to user (optional) |
| `timeout-seconds` | number | No | Payment timeout in seconds (default: 900 = 15min) |

## Payment Flow Implementation

### 1. Generate Payment Request

```javascript
class PaymentBookingWidget {
  generatePaymentRequest() {
    return {
      booking_id: crypto.randomUUID(),
      wallet_address: this.config.walletAddress,
      amount_usdc: this.config.paymentAmountUSDC,
      amount_lamports: this.usdcToLamports(this.config.paymentAmountUSDC),
      network: this.config.paymentNetwork,
      memo: JSON.stringify({
        booking_id: this.bookingId,
        type: "calendly_booking",
        timestamp: Date.now()
      }),
      expires_at: Date.now() + (this.config.timeoutSeconds * 1000)
    };
  }

  usdcToLamports(usdcAmount) {
    // USDC has 6 decimals
    return Math.floor(usdcAmount * 1_000_000);
  }
}
```

### 2. Generate QR Code

Use Solana Pay URI specification:

```
solana:<recipient>?amount=<amount>&spl-token=<mint>&memo=<memo>&label=<label>&message=<message>
```

Example:
```
solana:YourWalletAddress?amount=10&spl-token=EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v&memo=%7B%22booking_id%22%3A%22abc-123%22%7D&label=Consultation%20Booking&message=Pay%2010%20USDC%20to%20book%20your%20consultation
```

**Notes:**
- `spl-token`: USDC mint address (mainnet: `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`)
- `memo`: URL-encoded JSON with booking_id
- `amount`: USDC amount (human-readable, not lamports)

### 3. Subscribe to Transaction Stream

```javascript
class PaymentMonitor {
  constructor(forohtooServer, walletAddress, matcher) {
    this.serverUrl = forohtooServer;
    this.walletAddress = walletAddress;
    this.matcher = matcher;
    this.eventSource = null;
  }

  async waitForPayment(timeoutMs) {
    return new Promise((resolve, reject) => {
      const streamUrl = `${this.serverUrl}/api/v1/stream/transactions/${this.walletAddress}`;
      this.eventSource = new EventSource(streamUrl);

      const timeout = setTimeout(() => {
        this.stop();
        reject(new Error('Payment timeout'));
      }, timeoutMs);

      this.eventSource.addEventListener('connected', (e) => {
        console.log('Connected to payment stream:', JSON.parse(e.data));
      });

      this.eventSource.addEventListener('transaction', (e) => {
        const txn = JSON.parse(e.data);

        if (this.matcher(txn)) {
          clearTimeout(timeout);
          this.stop();
          resolve(txn);
        }
      });

      this.eventSource.onerror = (err) => {
        clearTimeout(timeout);
        this.stop();
        reject(new Error('SSE connection error'));
      };
    });
  }

  stop() {
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }
  }
}
```

### 4. Transaction Matching Logic

```javascript
function createPaymentMatcher(bookingId, expectedAmountLamports, allowOverpayment = false) {
  return function(txn) {
    // 1. Check amount
    const amountMatches = allowOverpayment
      ? txn.amount >= expectedAmountLamports
      : txn.amount === expectedAmountLamports;

    if (!amountMatches) {
      return false;
    }

    // 2. Parse memo as JSON
    let memo;
    try {
      memo = JSON.parse(txn.memo || '{}');
    } catch (e) {
      return false;
    }

    // 3. Check booking_id in memo
    if (memo.booking_id !== bookingId) {
      return false;
    }

    // 4. Optional: Verify token mint is USDC
    // (forohtoo should provide token_mint in transaction data)
    // if (txn.token_mint !== EXPECTED_USDC_MINT) {
    //   return false;
    // }

    return true;
  };
}
```

### 5. Unlock Calendly Widget

```javascript
class BookingUI {
  showPaymentPending(paymentRequest) {
    // Show QR code
    this.generateQRCode(paymentRequest);

    // Show payment instructions
    this.showInstructions(paymentRequest);

    // Show countdown timer
    if (this.config.showCountdown) {
      this.startCountdown(paymentRequest.expires_at);
    }

    // Hide Calendly
    this.hideCalendly();
  }

  showPaymentSuccess(transaction) {
    // Hide QR code
    this.hideQRCode();

    // Show success message
    this.showSuccessMessage(transaction);

    // Reveal Calendly widget
    this.showCalendly();
  }

  showCalendly() {
    // Option 1: Calendly inline widget
    Calendly.initInlineWidget({
      url: this.config.calendlyUrl,
      parentElement: document.getElementById('calendly-container')
    });

    // Option 2: Direct iframe embed
    const iframe = document.createElement('iframe');
    iframe.src = this.config.calendlyUrl;
    iframe.width = '100%';
    iframe.height = '700px';
    iframe.frameBorder = '0';
    document.getElementById('calendly-container').appendChild(iframe);
  }
}
```

## State Management

The widget maintains these states:

```javascript
const STATES = {
  INIT: 'init',                    // Initial load
  PAYMENT_PENDING: 'pending',      // QR code shown, waiting for payment
  PAYMENT_CONFIRMED: 'confirmed',  // Payment received, verified
  BOOKING_ACTIVE: 'booking',       // Calendly widget shown
  ERROR: 'error',                  // Payment timeout or connection error
  EXPIRED: 'expired'               // Payment request expired
};
```

State transitions:

```
INIT → PAYMENT_PENDING → PAYMENT_CONFIRMED → BOOKING_ACTIVE
  ↓           ↓                ↓
ERROR ← ─ ─ ─ ┴ ─ ─ ─ ─ ─ ─ ─ ┘
  ↓           ↓
EXPIRED ← ─ ─ ┘
```

## Security Considerations

### CORS Configuration

Forohtoo server must allow CORS requests from your website domain:

```go
// In forohtoo server configuration
corsMiddleware := func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        origin := r.Header.Get("Origin")

        // Whitelist your domains
        allowedOrigins := []string{
            "https://yourdomain.com",
            "https://www.yourdomain.com",
        }

        for _, allowed := range allowedOrigins {
            if origin == allowed {
                w.Header().Set("Access-Control-Allow-Origin", origin)
                w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
                break
            }
        }

        if r.Method == "OPTIONS" {
            w.WriteHeader(http.StatusOK)
            return
        }

        next.ServeHTTP(w, r)
    })
}
```

### Transaction Validation

**Client-side validation (defense in depth):**
1. Verify booking_id matches generated ID
2. Verify amount matches or exceeds expected amount
3. Verify transaction is recent (within timeout window)
4. Verify memo format is valid JSON

**Server-side validation (if building backend):**
1. Store generated booking_ids in database with expiry
2. Verify transaction on-chain (query Solana RPC directly)
3. Prevent double-booking (mark booking_id as used)
4. Rate limit booking attempts per IP

### Privacy Considerations

- Booking IDs are generated client-side (privacy-preserving)
- No personal information sent to forohtoo (only wallet address)
- Transaction memo contains only booking_id, no PII
- Consider: Store booking_id → user_email mapping server-side after payment

### Attack Vectors & Mitigations

**1. Payment Reuse Attack**
- **Threat**: User pays once, reuses transaction for multiple bookings
- **Mitigation**:
  - Generate unique booking_id per attempt
  - Server-side: Track used booking_ids, reject duplicates
  - Add timestamp to memo, reject old transactions

**2. Race Condition**
- **Threat**: Multiple tabs/users claim same transaction
- **Mitigation**:
  - First-match wins (client-side)
  - Server-side: Atomic booking_id redemption in database

**3. Payment Front-Running**
- **Threat**: Attacker sees pending transaction, copies memo
- **Mitigation**:
  - Use unpredictable booking_ids (UUIDs)
  - Short expiry windows (5-15 minutes)

**4. SSE Hijacking**
- **Threat**: Attacker subscribes to your wallet's transaction stream
- **Mitigation**:
  - This is acceptable! Public wallet = public transactions
  - Booking_ids are unguessable (UUID v4 = 2^122 space)
  - Consider: Optional authentication on SSE endpoints

**5. Underpayment**
- **Threat**: User sends less than required amount
- **Mitigation**:
  - Exact amount checking (or >= with allowOverpayment flag)
  - Show clear error message for underpayments

## Error Handling

### Connection Errors

```javascript
class ErrorHandler {
  handleSSEError(error) {
    if (error.message === 'Payment timeout') {
      this.showTimeoutMessage();
      this.offerRetry();
    } else if (error.message === 'SSE connection error') {
      this.showConnectionError();
      this.attemptReconnect();
    }
  }

  attemptReconnect(maxRetries = 3) {
    let attempts = 0;
    const retry = () => {
      if (attempts >= maxRetries) {
        this.showFatalError();
        return;
      }

      attempts++;
      this.showRetrying(attempts, maxRetries);

      // Exponential backoff
      const delay = Math.min(1000 * Math.pow(2, attempts), 10000);
      setTimeout(() => {
        this.monitor.waitForPayment(this.timeoutMs)
          .then(txn => this.handlePaymentSuccess(txn))
          .catch(err => retry());
      }, delay);
    };

    retry();
  }
}
```

### Payment Validation Errors

Show specific error messages:
- "Payment amount incorrect (expected X USDC, received Y USDC)"
- "Payment memo missing or invalid"
- "Payment expired (please generate a new booking request)"
- "Transaction already used for another booking"

## Testing Strategy

### Unit Tests (Jest/Mocha)

```javascript
describe('PaymentMatcher', () => {
  it('should match valid payment transaction', () => {
    const matcher = createPaymentMatcher('test-id-123', 10_000000, false);
    const txn = {
      amount: 10_000000,
      memo: JSON.stringify({ booking_id: 'test-id-123' })
    };
    expect(matcher(txn)).toBe(true);
  });

  it('should reject underpayment', () => {
    const matcher = createPaymentMatcher('test-id-123', 10_000000, false);
    const txn = {
      amount: 5_000000,
      memo: JSON.stringify({ booking_id: 'test-id-123' })
    };
    expect(matcher(txn)).toBe(false);
  });

  it('should accept overpayment when allowed', () => {
    const matcher = createPaymentMatcher('test-id-123', 10_000000, true);
    const txn = {
      amount: 15_000000,
      memo: JSON.stringify({ booking_id: 'test-id-123' })
    };
    expect(matcher(txn)).toBe(true);
  });
});
```

### Integration Tests (Manual)

**Devnet Testing:**
1. Configure widget for devnet
2. Register devnet wallet in forohtoo
3. Use devnet faucet to get SOL
4. Use devnet USDC faucet
5. Send test payment matching booking_id
6. Verify widget unlocks Calendly

**Mainnet Testing (Small Amounts):**
1. Configure widget for mainnet with minimal amount (0.01 USDC)
2. Complete end-to-end flow
3. Verify on Solscan/Solana Explorer

### Browser Compatibility Testing

Test on:
- Chrome/Edge (Chromium)
- Firefox
- Safari (macOS/iOS)
- Mobile browsers (iOS Safari, Chrome Mobile)

Verify:
- EventSource API support (all modern browsers)
- crypto.randomUUID() support (polyfill for older browsers)
- QR code rendering
- Calendly widget embedding

## Deployment

Just copy the files to your web server:

```bash
# Copy widget files to your website
cp payment-booking-widget.js /path/to/your/website/
cp payment-booking-widget.css /path/to/your/website/
```

Then include them in your HTML (see Usage Example section above).

## MVP Scope

This widget is intentionally kept simple and focused:

**Core Features:**
- ✅ Basic payment → Calendly flow
- ✅ SSE-based transaction monitoring
- ✅ QR code generation (Solana Pay URI)
- ✅ Transaction validation (amount + booking_id)
- ✅ Timeout handling
- ✅ Basic error states

**Out of Scope (Keep It Simple):**
- ❌ Backend API/database
- ❌ Email capture or notifications
- ❌ Analytics dashboards
- ❌ Multiple payment tokens
- ❌ Custom branding/theming
- ❌ Mobile wallet deep linking
- ❌ Refunds or cancellations

The widget is a simple HTML+JS snippet that monitors payments via forohtoo and unlocks Calendly. Nothing more.

## Dependencies

**JavaScript Libraries:**
- **qrcodejs** (~5KB) - QR code generation
  - Alternative: qrcode-generator, qr.js

**External Services:**
- **forohtoo** - Payment monitoring (must be running and accessible)
- **Calendly** - Booking/scheduling

**Browser APIs:**
- EventSource (SSE) - Supported in all modern browsers
- crypto.randomUUID() - Modern browsers only (use polyfill if needed)

## File Structure (Simple)

```
payment-booking-widget/
├── payment-booking-widget.js    # Single JS file with all logic
├── payment-booking-widget.css   # Basic styles
├── example.html                 # Usage example
└── README.md                    # Usage instructions
```

Keep it simple - one JS file, one CSS file, one example. No build process required.

## Usage Example

```html
<!DOCTYPE html>
<html>
<head>
  <link rel="stylesheet" href="payment-booking-widget.css">
  <script src="https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js"></script>
  <script src="https://assets.calendly.com/assets/external/widget.js"></script>
</head>
<body>
  <div id="payment-booking-widget"
       data-forohtoo-server="https://your-forohtoo-server.com"
       data-wallet-address="YourSolanaWalletAddress"
       data-payment-amount-usdc="10"
       data-payment-network="mainnet"
       data-calendly-url="https://calendly.com/yourname/30min"
       data-booking-title="30 Minute Consultation">
  </div>

  <script src="payment-booking-widget.js"></script>
  <script>
    // Widget auto-initializes from data attributes
    // Or manually:
    // PaymentBookingWidget.init('#payment-booking-widget');
  </script>
</body>
</html>
```

That's it. Simple and self-contained.
