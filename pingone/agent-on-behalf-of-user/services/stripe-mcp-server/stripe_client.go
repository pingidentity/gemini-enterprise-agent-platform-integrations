package main

import (
	"fmt"
	"strings"

	stripe "github.com/stripe/stripe-go/v79"
	stripecustomer "github.com/stripe/stripe-go/v79/customer"
	"github.com/stripe/stripe-go/v79/paymentintent"
	stripeprice "github.com/stripe/stripe-go/v79/price"
	stripeproduct "github.com/stripe/stripe-go/v79/product"
)

// fetchProductsFromStripe lists all active Stripe products with their default prices.
func fetchProductsFromStripe() (string, error) {
	params := &stripe.ProductListParams{Active: stripe.Bool(true)}
	params.AddExpand("data.default_price")
	iter := stripeproduct.List(params)

	var lines []string
	for iter.Next() {
		p := iter.Product()
		priceStr := "no price set"
		if p.DefaultPrice != nil {
			priceStr = fmt.Sprintf("$%.2f %s", float64(p.DefaultPrice.UnitAmount)/100, p.DefaultPrice.Currency)
		}
		lines = append(lines, fmt.Sprintf("id=%s name=%q price=%s", p.ID, p.Name, priceStr))
	}
	if err := iter.Err(); err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "no active products found in Stripe catalog", nil
	}
	return strings.Join(lines, "\n"), nil
}

// fetchProduct returns details for a single Stripe product including its default price.
func fetchProduct(productID string) (string, error) {
	params := &stripe.ProductParams{}
	params.AddExpand("default_price")
	p, err := stripeproduct.Get(productID, params)
	if err != nil {
		return "", err
	}
	priceStr := "no price set"
	if p.DefaultPrice != nil {
		priceStr = fmt.Sprintf("$%.2f %s", float64(p.DefaultPrice.UnitAmount)/100, p.DefaultPrice.Currency)
	}
	return fmt.Sprintf("id=%s name=%q description=%q price=%s active=%t",
		p.ID, p.Name, p.Description, priceStr, p.Active), nil
}

// lookupCustomerByEmail finds a Stripe customer by email, expanding the default
// payment method so callers can use it without a second API call.
func lookupCustomerByEmail(email string) (*stripe.Customer, error) {
	params := &stripe.CustomerListParams{Email: stripe.String(email)}
	params.AddExpand("data.invoice_settings.default_payment_method")
	iter := stripecustomer.List(params)

	if iter.Next() {
		return iter.Customer(), iter.Err()
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no Stripe customer found with email %q — they must register and save a payment method first", email)
}

// resolvePaymentMethod returns the customer's preferred payment method ID.
// Checks invoice default first, then falls back to the first attached card.
func resolvePaymentMethod(customer *stripe.Customer) string {
	if customer.InvoiceSettings.DefaultPaymentMethod != nil {
		return customer.InvoiceSettings.DefaultPaymentMethod.ID
	}
	pmParams := &stripe.CustomerListPaymentMethodsParams{
		Customer: stripe.String(customer.ID),
	}
	pmIter := stripecustomer.ListPaymentMethods(pmParams)
	if pmIter.Next() {
		return pmIter.PaymentMethod().ID
	}
	return ""
}

// chargeProduct creates a confirmed PaymentIntent for the given product and quantity.
// Returns a human-readable receipt string.
func chargeProduct(productID, customerID, paymentMethodID, email string, quantity int64) (string, error) {
	prod, err := stripeproduct.Get(productID, nil)
	if err != nil {
		return "", fmt.Errorf("product lookup: %w", err)
	}
	if prod.DefaultPrice == nil {
		return "", fmt.Errorf("product %q has no default price set in Stripe", productID)
	}

	price, err := stripeprice.Get(prod.DefaultPrice.ID, nil)
	if err != nil {
		return "", fmt.Errorf("price lookup: %w", err)
	}

	totalCents := price.UnitAmount * quantity

	params := &stripe.PaymentIntentParams{
		Amount:        stripe.Int64(totalCents),
		Currency:      stripe.String(string(price.Currency)),
		Customer:      stripe.String(customerID),
		PaymentMethod: stripe.String(paymentMethodID),
		ReceiptEmail:  stripe.String(email),
		Confirm:       stripe.Bool(true),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled:        stripe.Bool(true),
			AllowRedirects: stripe.String("never"),
		},
	}
	params.AddMetadata("product_id", prod.ID)
	params.AddMetadata("product_name", prod.Name)
	params.AddMetadata("quantity", fmt.Sprintf("%d", quantity))

	pi, err := paymentintent.New(params)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"payment_intent_id=%s status=%s amount_charged=$%.2f %s item=%q quantity=%d receipt_sent_to=%s",
		pi.ID, pi.Status, float64(totalCents)/100, price.Currency, prod.Name, quantity, email,
	), nil
}
