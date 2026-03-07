# FAQ

## Binance Error `code=-4061`

Error message:
`Order's position side does not match user's setting`

Reason:
Your account position mode and the order side settings do not match.

How to fix:
1. Open Binance Futures settings for your account.
2. Choose one position mode and keep it consistent with your bot config.
3. If your bot is configured for one-way mode, disable hedge mode in Binance.
4. If your bot is configured for hedge mode, enable hedge mode in Binance and set position side correctly.
5. Restart the bot after updating the exchange mode.

If the issue continues, open an issue in the project repository.
