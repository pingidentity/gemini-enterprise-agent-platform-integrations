package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RestockInput struct {
	ProductID string `json:"product_id" jsonschema:"the product SKU to restock"`
	Quantity  int    `json:"quantity" jsonschema:"number of units to order"`
	Region    string `json:"region" jsonschema:"target fulfillment region"`
}

type RestockOutput struct {
	Status    string `json:"status"`
	OrderID   string `json:"order_id"`
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

// handleRestock is a mock — it returns a hardcoded accepted order.
func handleRestock(_ context.Context, _ *mcp.CallToolRequest, in RestockInput) (*mcp.CallToolResult, RestockOutput, error) {
	log.Printf("[SupplyChain] tools/call restock — %d units of %s for region %s", in.Quantity, in.ProductID, in.Region)
	return nil, RestockOutput{Status: "accepted", OrderID: "ORD-20240101-001", ProductID: in.ProductID, Quantity: in.Quantity}, nil
}
