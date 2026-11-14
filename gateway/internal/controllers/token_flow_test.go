package controllers

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"assterisk/gateway/internal/databases"
)

func TestGenerateMasterToken(t *testing.T) {
	re := regexp.MustCompile(`^GOTD:[A-Za-z]{32}$`)
	for i := 0; i < 50; i++ {
		tok := GenerateMasterToken()
		if !re.MatchString(tok) {
			t.Fatalf("Token generated does not match format: %s", tok)
		}
	}
}

func TestComputeTokenSHA(t *testing.T) {
	master := "GOTD:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef"
	userID := int64(123456789)

	sha1 := ComputeTokenSHA(master, userID)
	sha2 := ComputeTokenSHA(master, userID)

	if sha1 != sha2 {
		t.Fatalf("SHA computation must be deterministic: %s != %s", sha1, sha2)
	}

	re := regexp.MustCompile(`^GOTD_SHA:[a-f0-9]{64}$`)
	if !re.MatchString(sha1) {
		t.Fatalf("SHA token does not match expected format: %s", sha1)
	}
}

func TestParseDurationStr(t *testing.T) {
	cases := []struct {
		input    string
		expected time.Duration
		hasError bool
	}{
		{"2h4j", 2*24*time.Hour + 4*time.Hour, false},
		{"30h", 30 * 24 * time.Hour, false},
		{"1h30m", 24*time.Hour + 30*time.Minute, false},
		{"5j10m", 5*time.Hour + 10*time.Minute, false},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, tc := range cases {
		d, err := ParseDurationStr(tc.input)
		if tc.hasError {
			if err == nil {
				t.Fatalf("Expected error for input %q, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Fatalf("Unexpected error for %q: %v", tc.input, err)
			}
			if d != tc.expected {
				t.Fatalf("For %q expected %v, got %v", tc.input, tc.expected, d)
			}
		}
	}
}

func TestTokenDatabaseOperations(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_tokens.db")

	client, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("Failed to init SQLite: %v", err)
	}
	defer client.DB.Close()

	// 1. Save and Get Master Token
	masterTok := GenerateMasterToken()
	tm := &databases.TokenMaster{
		Token:           masterTok,
		Tier:            "Pro",
		DurationSeconds: 86400 * 2,
		IsShared:        false,
		ClaimCount:      0,
		CreatedBy:       1935806583,
		CreatedAt:       time.Now(),
		Status:          "active",
	}

	if err := client.SaveTokenMaster(ctx, tm); err != nil {
		t.Fatalf("SaveTokenMaster err: %v", err)
	}

	fetched, err := client.GetTokenMaster(ctx, masterTok)
	if err != nil || fetched == nil {
		t.Fatalf("GetTokenMaster err: %v, fetched: %v", err, fetched)
	}
	if fetched.Tier != "Pro" || fetched.DurationSeconds != 86400*2 {
		t.Fatalf("Mismatch fetched token: %+v", fetched)
	}

	// 2. Increment Claim Count
	if err := client.IncrementTokenMasterClaim(ctx, masterTok); err != nil {
		t.Fatalf("IncrementTokenMasterClaim err: %v", err)
	}
	fetched2, _ := client.GetTokenMaster(ctx, masterTok)
	if fetched2.ClaimCount != 1 {
		t.Fatalf("Expected claim count 1, got %d", fetched2.ClaimCount)
	}

	// 3. User License Save & Retrieve by SHA
	sha := ComputeTokenSHA(masterTok, 112233)
	lic := &databases.UserLicense{
		TokenSHA:           sha,
		MasterToken:        masterTok,
		OwnerChatID:        112233,
		UserbotID:          112233,
		Tier:               "Pro",
		CanCustomWatermark: false,
		ExpiresAt:          time.Now().Add(48 * time.Hour),
		CreatedAt:          time.Now(),
		Status:             "active",
	}

	if err := client.SaveUserLicense(ctx, lic); err != nil {
		t.Fatalf("SaveUserLicense err: %v", err)
	}

	fetchedLic, err := client.GetUserLicenseBySHA(ctx, sha)
	if err != nil || fetchedLic == nil {
		t.Fatalf("GetUserLicenseBySHA err: %v, lic: %v", err, fetchedLic)
	}
	if fetchedLic.Tier != "Pro" || fetchedLic.OwnerChatID != 112233 {
		t.Fatalf("Mismatch fetched license: %+v", fetchedLic)
	}

	// 4. Get Active License by Chat ID
	activeLic, err := client.GetActiveUserLicense(ctx, 112233)
	if err != nil || activeLic == nil {
		t.Fatalf("GetActiveUserLicense err: %v, lic: %v", err, activeLic)
	}
	if activeLic.TokenSHA != sha {
		t.Fatalf("Expected SHA %s, got %s", sha, activeLic.TokenSHA)
	}

	// 5. Relink user data to new chat ID
	if err := client.RelinkUserData(ctx, 112233, 998877); err != nil {
		t.Fatalf("RelinkUserData err: %v", err)
	}
}
