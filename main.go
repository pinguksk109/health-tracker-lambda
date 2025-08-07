package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type WebhookRequest struct {
	Destination string `json:"destination"`
	Events      []struct {
		Type    string `json:"type"`
		Message *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"message"`
		Timestamp int64 `json:"timestamp"`
	} `json:"events"`
}

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var wb WebhookRequest
	if err := json.Unmarshal([]byte(req.Body), &wb); err != nil {
		log.Printf("JSON parse error: %v", err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest}, nil
	}
	if len(wb.Events) == 0 {
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	ev := wb.Events[0]
	if ev.Type != "message" || ev.Message == nil || ev.Message.Type != "text" {
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	lines := strings.Split(strings.TrimSpace(ev.Message.Text), "\n")
	if len(lines) != 2 {
		log.Printf("Invalid format, expected two lines: %q", ev.Message.Text)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	evtTime := time.Unix(ev.Timestamp/1000, 0)
	date := evtTime.Format("2006-01-02")

	weight, err := strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
	if err != nil {
		log.Printf("Weight parse error (%q): %v", lines[0], err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}
	bodyFat, err := strconv.ParseFloat(strings.TrimSpace(lines[1]), 64)
	if err != nil {
		log.Printf("BodyFat parse error (%q): %v", lines[1], err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	if err := appendToSheet(ctx, date, weight, bodyFat); err != nil {
		log.Printf("Append failed: %v", err)
	}

	return events.APIGatewayProxyResponse{StatusCode: http.StatusOK, Body: "OK"}, nil
}

func appendToSheet(ctx context.Context, date string, weight, bodyFat float64) error {
	credFile := os.Getenv("GOOGLE_CREDENTIALS_FILE")
	b, err := os.ReadFile(credFile)
	if err != nil {
		return fmt.Errorf("cannot read credentials file: %w", err)
	}
	cfg, err := google.JWTConfigFromJSON(b, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("parse credentials: %w", err)
	}
	client := cfg.Client(ctx)
	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("create sheets service: %w", err)
	}

	ssID := os.Getenv("SPREADSHEET_ID")
	if ssID == "" {
		ssID = "YOUR_SPREADSHEET_ID"
	}
	writeRange := os.Getenv("SHEET_RANGE")
	if writeRange == "" {
		writeRange = "シート1!A:C"
	}

	vr := &sheets.ValueRange{
		Values: [][]interface{}{
			{date, weight, bodyFat},
		},
	}
	_, err = srv.Spreadsheets.Values.Append(ssID, writeRange, vr).
		ValueInputOption("USER_ENTERED").
		InsertDataOption("INSERT_ROWS").
		Do()
	if err != nil {
		return fmt.Errorf("sheets append: %w", err)
	}
	log.Printf("Appended: %s, %.2f, %.2f", date, weight, bodyFat)
	return nil
}

func main() {
	_ = godotenv.Load(".env")
	lambda.Start(handler)
}
