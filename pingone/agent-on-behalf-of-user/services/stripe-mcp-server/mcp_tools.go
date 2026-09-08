package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	stripe "github.com/stripe/stripe-go/v79"
	stripecustomer "github.com/stripe/stripe-go/v79/customer"
)

// registerStripeMcpTools adds all Stripe MCP tools to this MCP server.
func registerStripeMcpTools(s *server.MCPServer) {
	s.AddTool(listStripeProductsTool())
	s.AddTool(getStripeProductTool())
	s.AddTool(getStripeCustomerTool())
	s.AddTool(createStripePaymentIntentTool())
}

func listStripeProductsTool() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("list_stripe_products",
		mcp.WithDescription("List all active products from the Stripe catalog, including their prices."),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		email, _ := ctx.Value(ctxKeyCallerEmail).(string)
		log.Printf("[SupplyChain] tool=list_stripe_products — caller=%s", email)
		products, err := fetchProductsFromStripe()
		if err != nil {
			log.Printf("[SupplyChain] tool=list_stripe_products — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("stripe error: %v", err)), nil
		}
		log.Printf("[SupplyChain] tool=list_stripe_products — success: caller=%s", email)
		return mcp.NewToolResultText(products), nil
	}
	return tool, handler
}

func getStripeProductTool() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("get_stripe_product",
		mcp.WithDescription("Get details for a specific Stripe product by product ID."),
		mcp.WithString("product_id",
			mcp.Required(),
			mcp.Description("The Stripe product ID (e.g. prod_ABC123)."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		email, _ := ctx.Value(ctxKeyCallerEmail).(string)
		productID, err := req.RequireString("product_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		log.Printf("[SupplyChain] tool=get_stripe_product — caller=%s product_id=%s", email, productID)
		result, err := fetchProduct(productID)
		if err != nil {
			log.Printf("[SupplyChain] tool=get_stripe_product — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("stripe error: %v", err)), nil
		}
		log.Printf("[SupplyChain] tool=get_stripe_product — success caller=%s product_id=%s", email, productID)
		return mcp.NewToolResultText(result), nil
	}
	return tool, handler
}

func getStripeCustomerTool() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("get_stripe_customer",
		mcp.WithDescription("Look up the authenticated user's Stripe customer record and return their saved payment method details (card brand and last 4 digits). Call this before create_payment_intent to confirm the card with the user."),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		customerEmail, ok := ctx.Value(ctxKeyCallerEmail).(string)
		if !ok || customerEmail == "" {
			return mcp.NewToolResultError("could not determine user email from auth token"), nil
		}
		log.Printf("[SupplyChain] tool=get_stripe_customer — caller=%s", customerEmail)

		customer, err := lookupCustomerByEmail(customerEmail)
		if err != nil {
			log.Printf("[SupplyChain] tool=get_stripe_customer — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("customer lookup error: %v", err)), nil
		}

		pmParams := &stripe.CustomerListPaymentMethodsParams{
			Customer: stripe.String(customer.ID),
		}
		pmIter := stripecustomer.ListPaymentMethods(pmParams)
		if pmIter.Next() {
			pm := pmIter.PaymentMethod()
			if pm.Card != nil {
				log.Printf("[SupplyChain] tool=get_stripe_customer — success: caller=%s customer_id=%s card=****%s", customerEmail, customer.ID, pm.Card.Last4)
				return mcp.NewToolResultText(fmt.Sprintf(
					"customer_id=%s email=%s card_brand=%s card_last4=%s card_exp=%02d/%d",
					customer.ID, customer.Email,
					pm.Card.Brand, pm.Card.Last4,
					pm.Card.ExpMonth, pm.Card.ExpYear,
				)), nil
			}
		}
		if err := pmIter.Err(); err != nil {
			log.Printf("[SupplyChain] tool=get_stripe_customer — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("payment method lookup error: %v", err)), nil
		}
		log.Printf("[SupplyChain] tool=get_stripe_customer — no payment method on file for %s", customerEmail)
		return mcp.NewToolResultError(fmt.Sprintf("no saved payment method on file for %s", customerEmail)), nil
	}
	return tool, handler
}

func createStripePaymentIntentTool() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("create_stripe_payment_intent",
		mcp.WithDescription("Purchase a product on behalf of the authenticated user using their saved payment method on file. Stripe will send a receipt to their email. IMPORTANT: Always call get_stripe_customer first to retrieve and confirm the card on file with the user before calling this tool."),
		mcp.WithString("product_id",
			mcp.Required(),
			mcp.Description("The Stripe product ID to purchase."),
		),
		mcp.WithNumber("quantity",
			mcp.Description("Number of units to purchase (default 1)."),
		),
		mcp.WithNumber("total_price",
			mcp.Required(),
			mcp.Description("Total price in dollars for the purchase (unit price × quantity). For example, if a product costs $100.00 and quantity is 1, total_price is 100."),
		),
		mcp.WithString("currency",
			mcp.Required(),
			mcp.Description("Three-letter ISO 4217 currency code (e.g. \"usd\", \"eur\")."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		customerEmail, ok := ctx.Value(ctxKeyCallerEmail).(string)
		if !ok || customerEmail == "" {
			return mcp.NewToolResultError("could not determine user email from auth token"), nil
		}

		productID, err := req.RequireString("product_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		quantity := int64(req.GetFloat("quantity", 1))
		if quantity < 1 {
			quantity = 1
		}
		log.Printf("[SupplyChain] tool=create_stripe_payment_intent — caller=%s product_id=%s quantity=%d total_price=%.2f", customerEmail, productID, quantity, req.GetFloat("total_price", 0))

		customer, err := lookupCustomerByEmail(customerEmail)
		if err != nil {
			log.Printf("[SupplyChain] tool=create_stripe_payment_intent — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("customer lookup error: %v", err)), nil
		}

		paymentMethodID := resolvePaymentMethod(customer)
		if paymentMethodID == "" {
			log.Printf("[SupplyChain] tool=create_stripe_payment_intent — no payment method for %s", customerEmail)
			return mcp.NewToolResultError(fmt.Sprintf("no saved payment method on file for %s — customer must add a card first", customerEmail)), nil
		}

		receipt, err := chargeProduct(productID, customer.ID, paymentMethodID, customerEmail, quantity)
		if err != nil {
			log.Printf("[SupplyChain] tool=create_stripe_payment_intent — error: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("stripe payment error: %v", err)), nil
		}
		log.Printf("[SupplyChain] tool=create_stripe_payment_intent — success: caller=%s product_id=%s quantity=%d", customerEmail, productID, quantity)
		return mcp.NewToolResultText(receipt), nil
	}
	return tool, handler
}
