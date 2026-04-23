package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/432539/gpt2api/internal/account"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/scheduler"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

func TestAcquireChatRequirementsThinkingDispatchesRequirePaid(t *testing.T) {
	var calls []scheduler.DispatchOptions
	h := &Handler{
		dispatchChatLeaseFunc: func(_ context.Context, opt scheduler.DispatchOptions) (*scheduler.Lease, error) {
			calls = append(calls, cloneDispatchOptions(opt))
			return nil, scheduler.ErrNoAvailable
		},
	}

	lease, cli, cr, excluded, err := h.acquireChatRequirements(context.Background(), "req-thinking", &modelpkg.Model{
		Slug: "gpt-5-thinking",
		Type: modelpkg.TypeChat,
	})
	if !errors.Is(err, scheduler.ErrNoAvailable) {
		t.Fatalf("err = %v, want ErrNoAvailable", err)
	}
	if lease != nil || cli != nil || cr != nil {
		t.Fatalf("unexpected return values: lease=%#v cli=%#v cr=%#v", lease, cli, cr)
	}
	if len(excluded) != 0 {
		t.Fatalf("excluded = %#v, want empty", excluded)
	}
	if len(calls) != 1 {
		t.Fatalf("dispatch calls = %d, want 1", len(calls))
	}
	if calls[0].ModelType != modelpkg.TypeChat {
		t.Fatalf("dispatch model_type = %q, want %q", calls[0].ModelType, modelpkg.TypeChat)
	}
	if !calls[0].RequirePaid {
		t.Fatalf("dispatch require_paid = false, want true")
	}
	if len(calls[0].ExcludeAccountIDs) != 0 {
		t.Fatalf("dispatch excluded = %#v, want empty", calls[0].ExcludeAccountIDs)
	}
}

func TestAcquireChatRequirementsRetriesFreePersona(t *testing.T) {
	leases := []*scheduler.Lease{
		{Account: &account.Account{ID: 11}},
		{Account: &account.Account{ID: 22}},
	}
	var dispatches []scheduler.DispatchOptions
	var freeMarks []uint64
	var aborts []uint64
	var rateLimited []uint64
	var dead []uint64
	nextLease := 0

	h := &Handler{
		dispatchChatLeaseFunc: func(_ context.Context, opt scheduler.DispatchOptions) (*scheduler.Lease, error) {
			dispatches = append(dispatches, cloneDispatchOptions(opt))
			if nextLease >= len(leases) {
				return nil, scheduler.ErrNoAvailable
			}
			lease := leases[nextLease]
			nextLease++
			return lease, nil
		},
		loadChatRequirementsFunc: func(_ context.Context, lease *scheduler.Lease) (*chatgpt.Client, *chatgpt.ChatRequirementsResp, error) {
			switch lease.Account.ID {
			case 11:
				return &chatgpt.Client{}, &chatgpt.ChatRequirementsResp{Persona: "chatgpt-freeaccount"}, nil
			case 22:
				return &chatgpt.Client{}, &chatgpt.ChatRequirementsResp{Persona: "chatgpt-paid", Token: "paid-token"}, nil
			default:
				t.Fatalf("unexpected account id %d", lease.Account.ID)
				return nil, nil, nil
			}
		},
		abortLeaseFunc: func(_ context.Context, lease *scheduler.Lease) error {
			aborts = append(aborts, lease.Account.ID)
			return nil
		},
		markFreeAccountFunc: func(_ context.Context, accountID uint64) {
			freeMarks = append(freeMarks, accountID)
		},
		markRateLimitedAccountFunc: func(_ context.Context, accountID uint64) {
			rateLimited = append(rateLimited, accountID)
		},
		markDeadAccountFunc: func(_ context.Context, accountID uint64) {
			dead = append(dead, accountID)
		},
	}

	lease, cli, cr, excluded, err := h.acquireChatRequirements(context.Background(), "req-retry", &modelpkg.Model{
		Slug: "gpt-5-thinking",
		Type: modelpkg.TypeChat,
	})
	if err != nil {
		t.Fatalf("acquireChatRequirements err = %v", err)
	}
	if lease != leases[1] {
		t.Fatalf("lease = %#v, want second lease", lease)
	}
	if cli == nil {
		t.Fatal("cli = nil, want paid client")
	}
	if cr == nil || cr.Persona != "chatgpt-paid" {
		t.Fatalf("cr = %#v, want paid persona", cr)
	}
	if len(dispatches) != 2 {
		t.Fatalf("dispatch calls = %d, want 2", len(dispatches))
	}
	if !dispatches[0].RequirePaid || !dispatches[1].RequirePaid {
		t.Fatalf("dispatch require_paid = %#v, want true on both calls", dispatches)
	}
	if len(dispatches[0].ExcludeAccountIDs) != 0 {
		t.Fatalf("first dispatch excluded = %#v, want empty", dispatches[0].ExcludeAccountIDs)
	}
	if _, ok := dispatches[1].ExcludeAccountIDs[11]; !ok {
		t.Fatalf("second dispatch excluded = %#v, want account 11", dispatches[1].ExcludeAccountIDs)
	}
	if len(freeMarks) != 1 || freeMarks[0] != 11 {
		t.Fatalf("mark free calls = %#v, want [11]", freeMarks)
	}
	if len(aborts) != 1 || aborts[0] != 11 {
		t.Fatalf("abort calls = %#v, want [11]", aborts)
	}
	if len(rateLimited) != 0 {
		t.Fatalf("rate limited marks = %#v, want none", rateLimited)
	}
	if len(dead) != 0 {
		t.Fatalf("dead marks = %#v, want none", dead)
	}
	if len(excluded) != 1 {
		t.Fatalf("excluded = %#v, want one free account", excluded)
	}
	if _, ok := excluded[11]; !ok {
		t.Fatalf("excluded = %#v, want account 11", excluded)
	}
}

func TestAcquireChatRequirementsReturnsUnavailableWhenPaidCandidatesExhausted(t *testing.T) {
	dispatchCalls := 0
	loadCalls := 0
	h := &Handler{
		dispatchChatLeaseFunc: func(_ context.Context, opt scheduler.DispatchOptions) (*scheduler.Lease, error) {
			dispatchCalls++
			if !opt.RequirePaid {
				t.Fatalf("dispatch require_paid = false, want true")
			}
			return nil, scheduler.ErrNoAvailable
		},
		loadChatRequirementsFunc: func(context.Context, *scheduler.Lease) (*chatgpt.Client, *chatgpt.ChatRequirementsResp, error) {
			loadCalls++
			return nil, nil, nil
		},
	}

	lease, cli, cr, excluded, err := h.acquireChatRequirements(context.Background(), "req-empty", &modelpkg.Model{
		Slug: "gpt-5-thinking",
		Type: modelpkg.TypeChat,
	})
	if !errors.Is(err, scheduler.ErrNoAvailable) {
		t.Fatalf("err = %v, want ErrNoAvailable", err)
	}
	if dispatchCalls != 1 {
		t.Fatalf("dispatch calls = %d, want 1", dispatchCalls)
	}
	if loadCalls != 0 {
		t.Fatalf("load calls = %d, want 0", loadCalls)
	}
	if lease != nil || cli != nil || cr != nil {
		t.Fatalf("unexpected return values: lease=%#v cli=%#v cr=%#v", lease, cli, cr)
	}
	if len(excluded) != 0 {
		t.Fatalf("excluded = %#v, want empty", excluded)
	}
}

func cloneDispatchOptions(opt scheduler.DispatchOptions) scheduler.DispatchOptions {
	cp := opt
	if len(opt.ExcludeAccountIDs) == 0 {
		cp.ExcludeAccountIDs = nil
		return cp
	}
	cp.ExcludeAccountIDs = make(map[uint64]struct{}, len(opt.ExcludeAccountIDs))
	for id := range opt.ExcludeAccountIDs {
		cp.ExcludeAccountIDs[id] = struct{}{}
	}
	return cp
}
