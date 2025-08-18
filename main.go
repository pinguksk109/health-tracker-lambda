package main

import (
	"bytes"
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

	// LineのWebhookは複数設定できないので、リクエストボディで分岐
	if strings.TrimSpace(ev.Message.Text) == "get" {
		data, err := getSheetData(ctx)
		if err != nil {
			log.Printf("Get sheet data failed: %v", err)
			return events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError}, nil
		}
		if err := sendLineMessage(strings.Join(data, "\n")); err != nil {
			log.Printf("Send line message failed: %v", err)
		}
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	lines := splitNonEmptyLines(ev.Message.Text)
	if len(lines) == 0 {
		log.Printf("Empty Message")
		return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest}, nil
	}

	if len(lines) > 4 {
		log.Printf("Invalid format (too many lines): %q", ev.Message.Text)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest}, nil
	}

	evtTime := time.Unix(ev.Timestamp/1000, 0)
	date := evtTime.Format("2006-01-02")

	weight, err := strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
	if err != nil {
		log.Printf("Weight parse error (%q): %v", lines[0], err)
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}

	var bodyFat, bodyWater, bodyMuscle *float64
	if len(lines) >= 2 {
		if v, ok := parseOptinalFloat(lines[1]); ok {
			bodyFat = &v
		}
	}
	if len(lines) >= 3 {
		if v, ok := parseOptinalFloat(lines[2]); ok {
			bodyWater = &v
		}
	}
	if len(lines) >= 4 {
		if v, ok := parseOptinalFloat(lines[3]); ok {
			bodyMuscle = &v
		}
	}

	if err := appendToSheet(ctx, date, weight, bodyFat, bodyWater, bodyMuscle); err != nil {
		log.Printf("Append failed: %v", err)
	}

	return events.APIGatewayProxyResponse{StatusCode: http.StatusOK, Body: "OK"}, nil
}

func appendToSheet(ctx context.Context, date string, weight float64, bodyFat, bodyWater, bodyMuscle *float64) error {
	srv, ssID, writeRange, err := initSheetsService(ctx)
	if err != nil {
		return err
	}

	row := []interface{}{date, weight}
	row = append(row, optCell(bodyFat))
	row = append(row, optCell(bodyWater))
	row = append(row, optCell(bodyMuscle))

	vr := &sheets.ValueRange{
		Values: [][]interface{}{row},
	}

	_, err = srv.Spreadsheets.Values.Append(ssID, writeRange, vr).
		ValueInputOption("USER_ENTERED").
		InsertDataOption("INSERT_ROWS").
		Do()
	if err != nil {
		return fmt.Errorf("sheets append: %w", err)
	}
	log.Printf("Appended: %s, weight=%.2f, body_fat=%v, body_water=%v, body_muscle=%v",
		date, weight, bodyFat, bodyWater, bodyMuscle)
	return nil
}

func optCell(p *float64) interface{} {
	if p == nil {
		return ""
	}
	return *p
}

func getSheetData(ctx context.Context) ([]string, error) {
	srv, ssID, readRange, err := initSheetsService(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := srv.Spreadsheets.Values.Get(ssID, readRange).Do()
	if err != nil {
		return nil, fmt.Errorf("read sheet: %w", err)
	}

	var results []string
	for _, row := range resp.Values {
		c := func(i int) string {
			if i < len(row) {
				if s, ok := row[i].(string); ok {
					if strings.TrimSpace(s) == "" {
						return "-"
					}
					return s
				}
				return fmt.Sprint(row[i])
			}
			return "-"
		}
		if len(row) >= 2 {
			results = append(results, fmt.Sprintf("%s: 体重=%s, 体脂肪率=%s, 体水分=%s, 筋肉量=%s",
				c(0), c(1), c(2), c(3), c(4)))
		}
	}
	return results, nil
}

func initSheetsService(ctx context.Context) (*sheets.Service, string, string, error) {
	credFile := os.Getenv("GOOGLE_CREDENTIALS_FILE")
	b, err := os.ReadFile(credFile)
	if err != nil {
		return nil, "", "", fmt.Errorf("cannot read credentials file: %w", err)
	}
	cfg, err := google.JWTConfigFromJSON(b, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse credentials: %w", err)
	}
	client := cfg.Client(ctx)
	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, "", "", fmt.Errorf("create sheets service: %w", err)
	}

	ssID := os.Getenv("SPREADSHEET_ID")
	if ssID == "" {
		return nil, "", "", fmt.Errorf("missing spreadsheet id")
	}
	readRange := os.Getenv("SHEET_RANGE")
	if readRange == "" {
		readRange = "シート1!A:E"
	}

	return srv, ssID, readRange, nil
}

func splitNonEmptyLines(s string) []string {
	raw := strings.Split(strings.TrimSpace(s), "\n")
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		t := strings.TrimSpace(r)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseOptinalFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		log.Printf("Optinal value parse error (%q): %v", s, err)
		return 0, false
	}
	return v, true
}

func sendLineMessage(message string) error {
	lineUserID := os.Getenv("LINE_USER_ID")
	token := os.Getenv("LINE_BEARER_TOKEN")
	if lineUserID == "" || token == "" {
		return fmt.Errorf("LINE_USER_ID or LINE_BEARER_TOKEN missing")
	}

	payload := map[string]interface{}{
		"to": lineUserID,
		"messages": []map[string]string{
			{"type": "text", "text": message},
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", "https://api.line.me/v2/bot/message/push", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LINE API return %d", resp.StatusCode)
	}
	return nil
}

func main() {
	_ = godotenv.Load(".env")
	lambda.Start(handler)
}
