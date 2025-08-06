package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
// 	log.Printf("Received Body: %s", req.Body)

// 	return events.APIGatewayProxyResponse{
// 		StatusCode:      200,
// 		Headers:         map[string]string{"Content-Type": "text/plain"},
// 		Body:            "OK",
// 		IsBase64Encoded: false,
// 	}, nil
// }

func handler(ctx context.Content, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var wb WebhookRequest
	if err := json.Unmarshal([]byte(req.Body), &wb); err != nil {
		log.Printf("JSON parse error: %v", err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest}, nil
	}
	if len(wb.Events) == 0 {
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	ev := wb.Event[0]
	if ev.Type != "message" || ev.Message == nil || ev.Message.Type != "text" {
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	text := ev.Message.Text
	parts := strings.Split(text, ",")
	if len(parts) != 3 {
		log.Printf("Invalid format: %q", text)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	dt, err := time.Parse("2006-01-02", strings.TrimSpace(parts[0]))
	if err != nil {
		log.Printf("Date parse error (%q): %v", parts[0], err)
	}
	date := dt.Format("2006-01-02")

	weight, err := strconv.ParseFloat(strings.TrimSpace((parts[1]), 64))
	if err != nil {
		log.Printf("Weight parse error (%q): %v", parts[1], err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}
	bodyFat, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), err)
	if err != nil {
		log.Printf("BodyFat parse error (%d): %v", parts[2], err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	if err := appendToSheet(ctx, date, weight, bodyFat); err != nil {
		log.Printf("Append failed: %v", err)
	}

	return events.APIGatewayProxyResponse{StatusCode: http.StatusOK, Body: "OK"}, nil
}

func appendToSheet(ctx context.Context, date string, weight, bodyFat float64) error {
	credFile := os.Getenv("GOOGLE_CREDENTIALS_FILE")
	b, _ := ioutil.ReadFile(credFile)
	cfg, _ := google.JWTConfigFromJSON(b, sheets.SpreadsheetsScope)
	client := cfg.Client(ctx)
	srv, _ := sheets.NewService(ctx, option.WithHTTPClient(client))

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
	_, err := srv.Spreadsheets.Values.Append(ssID, writeRange, vr).
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
	lambda.Start(handler)
}
