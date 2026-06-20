// Package sheets provides a thin wrapper around the Google Sheets API
// used to duplicate finished game results into an external spreadsheet.
package sheets

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const maxSheetTitleLen = 100

// Client talks to a single Google Sheets spreadsheet via a service account.
type Client struct {
	svc           *sheets.Service
	spreadsheetID string
}

// NewClient creates a Sheets API client authenticated via a service account JSON key file.
func NewClient(ctx context.Context, credentialsFile, spreadsheetID string) (*Client, error) {
	credentialsJSON, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("read credentials file: %w", err)
	}
	svc, err := sheets.NewService(ctx, option.WithCredentialsJSON(credentialsJSON))
	if err != nil {
		return nil, fmt.Errorf("create sheets service: %w", err)
	}
	return &Client{svc: svc, spreadsheetID: spreadsheetID}, nil
}

// CreateSheet adds a new sheet (tab) with the given title.
// The caller is responsible for ensuring the title is unique (e.g. by including
// the game ID), since the same title is later recomputed by callers to address
// the sheet without persisting any extra state.
func (c *Client) CreateSheet(ctx context.Context, title string) error {
	title = truncate(title, maxSheetTitleLen)

	req := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{{
			AddSheet: &sheets.AddSheetRequest{
				Properties: &sheets.SheetProperties{Title: title},
			},
		}},
	}
	if _, err := c.svc.Spreadsheets.BatchUpdate(c.spreadsheetID, req).Context(ctx).Do(); err != nil {
		return fmt.Errorf("add sheet %q: %w", title, err)
	}
	return nil
}

// WriteRows writes values starting at cell A1 of the given sheet.
// Uses ValueInputOption "RAW" so player-controlled strings (Telegram usernames/
// display names) are stored verbatim and never interpreted as Sheets formulas.
func (c *Client) WriteRows(ctx context.Context, sheetTitle string, values [][]any) error {
	rangeRef := fmt.Sprintf("%s!A1", quoteSheetTitle(sheetTitle))
	vr := &sheets.ValueRange{Values: values}
	_, err := c.svc.Spreadsheets.Values.Update(c.spreadsheetID, rangeRef, vr).
		ValueInputOption("RAW").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("write values to %q: %w", sheetTitle, err)
	}
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// quoteSheetTitle wraps a sheet title in single quotes for use in A1 notation,
// escaping any embedded single quotes by doubling them.
func quoteSheetTitle(title string) string {
	return "'" + strings.ReplaceAll(title, "'", "''") + "'"
}
