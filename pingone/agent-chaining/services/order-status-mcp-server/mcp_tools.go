package main

import (
	"context"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerOrderStatusTool(s *server.MCPServer) {
	tool := mcp.NewTool(
		"get_order_status",
		mcp.WithDescription("Get the current status of an order."),
		mcp.WithString("order_id", mcp.Required(), mcp.Description("Order ID, for example ORD-123")),
	)
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		orderID, err := request.RequireString("order_id")
		if err != nil || !validOrderID(orderID) {
			return mcp.NewToolResultError("order_id must match ORD-[0-9]+"), nil
		}
		orders := map[string][2]string{
			"ORD-123": {"shipped", "Order shipped and awaiting delivery."},
			"ORD-456": {"processing", "Order is being prepared."},
		}
		order, ok := orders[orderID]
		if !ok {
			log.Printf("[OrderStatusMCP] tool=get_order_status — order=%s not found", orderID)
			return mcp.NewToolResultError("order not found"), nil
		}
		result := map[string]any{
			"order_id":     orderID,
			"status":       order[0],
			"summary":      order[1],
			"last_updated": time.Now().UTC().Format(time.RFC3339),
		}
		caller, _ := ctx.Value(ctxKeyCallerSub{}).(string)
		log.Printf("[OrderStatusMCP] tool=get_order_status — caller=%s order=%s status=%s", caller, orderID, order[0])
		return mcp.NewToolResultStructured(result, ""), nil
	}
	s.AddTool(tool, handler)
}
