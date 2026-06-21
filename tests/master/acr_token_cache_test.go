package master_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"wasmcat/internal/master"
)

func TestACRTokenProviderCachesTokenForSameRepository(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	mintCalls := 0
	provider := master.NewACRTokenProviderWithOptions(
		func(context.Context, string, string) (string, time.Duration, error) {
			mintCalls++
			return "token-a", time.Hour, nil
		},
		func() time.Time {
			return now
		},
	)

	first, err := provider.Token(context.Background(), "registry", "team/echo")
	if err != nil {
		t.Fatalf("first token request failed: %v", err)
	}
	second, err := provider.Token(context.Background(), "registry", "team/echo")
	if err != nil {
		t.Fatalf("second token request failed: %v", err)
	}

	if first != "token-a" || second != "token-a" {
		t.Fatalf("expected cached token-a twice, got first=%q second=%q", first, second)
	}
	if mintCalls != 1 {
		t.Fatalf("expected one mint call, got %d", mintCalls)
	}
}

func TestACRTokenProviderSeparatesRepositories(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	mintCalls := 0
	provider := master.NewACRTokenProviderWithOptions(
		func(_ context.Context, _ string, repositoryName string) (string, time.Duration, error) {
			mintCalls++
			return "token-for-" + repositoryName, time.Hour, nil
		},
		func() time.Time {
			return now
		},
	)

	first, err := provider.Token(context.Background(), "registry", "team/echo")
	if err != nil {
		t.Fatalf("first repository token failed: %v", err)
	}
	second, err := provider.Token(context.Background(), "registry", "team/worker")
	if err != nil {
		t.Fatalf("second repository token failed: %v", err)
	}

	if first == second {
		t.Fatalf("expected separate repository tokens, got %q and %q", first, second)
	}
	if mintCalls != 2 {
		t.Fatalf("expected one mint per repository, got %d", mintCalls)
	}
}

func TestACRTokenProviderRefreshesNearExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	mintCalls := 0
	provider := master.NewACRTokenProviderWithOptions(
		func(context.Context, string, string) (string, time.Duration, error) {
			mintCalls++
			return "token-" + time.Duration(mintCalls).String(), time.Minute, nil
		},
		func() time.Time {
			return now
		},
	)

	first, err := provider.Token(context.Background(), "registry", "team/echo")
	if err != nil {
		t.Fatalf("first token request failed: %v", err)
	}

	// The provider refreshes before exact expiry so workers do not receive a
	// token that can expire while they are still downloading the ACR blob.
	now = now.Add(31 * time.Second)

	second, err := provider.Token(context.Background(), "registry", "team/echo")
	if err != nil {
		t.Fatalf("refresh token request failed: %v", err)
	}

	if first == second {
		t.Fatalf("expected a refreshed token, got %q twice", first)
	}
	if mintCalls != 2 {
		t.Fatalf("expected two mint calls after refresh, got %d", mintCalls)
	}
}

func TestACRTokenProviderDoesNotCacheMintFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	mintCalls := 0
	provider := master.NewACRTokenProviderWithOptions(
		func(context.Context, string, string) (string, time.Duration, error) {
			mintCalls++
			if mintCalls == 1 {
				return "", 0, errors.New("azure unavailable")
			}
			return "token-after-retry", time.Hour, nil
		},
		func() time.Time {
			return now
		},
	)

	if _, err := provider.Token(context.Background(), "registry", "team/echo"); err == nil {
		t.Fatal("expected first mint failure")
	}

	token, err := provider.Token(context.Background(), "registry", "team/echo")
	if err != nil {
		t.Fatalf("second token request failed: %v", err)
	}

	if token != "token-after-retry" {
		t.Fatalf("expected retry token, got %q", token)
	}
	if mintCalls != 2 {
		t.Fatalf("expected failed mint to be retried, got %d calls", mintCalls)
	}
}
