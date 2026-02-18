package core

import (
	"encoding/json"
	"testing"
)

func TestComputeRequestHash_Deterministic(t *testing.T) {
	body := json.RawMessage(`{"message":"hello","wsid":"ws-1"}`)
	h1 := ComputeRequestHash(body, "POST", "/v1/workspaces/ws-1/snapshots")
	h2 := ComputeRequestHash(body, "POST", "/v1/workspaces/ws-1/snapshots")
	if h1 != h2 {
		t.Fatalf("same input produced different hashes: %s vs %s", h1, h2)
	}
}

func TestComputeRequestHash_KeyOrderIrrelevant(t *testing.T) {
	body1 := json.RawMessage(`{"wsid":"ws-1","message":"hello"}`)
	body2 := json.RawMessage(`{"message":"hello","wsid":"ws-1"}`)
	h1 := ComputeRequestHash(body1, "POST", "/v1/workspaces/ws-1/snapshots")
	h2 := ComputeRequestHash(body2, "POST", "/v1/workspaces/ws-1/snapshots")
	if h1 != h2 {
		t.Fatalf("different key order produced different hashes: %s vs %s", h1, h2)
	}
}

func TestComputeRequestHash_DifferentBody(t *testing.T) {
	body1 := json.RawMessage(`{"message":"hello"}`)
	body2 := json.RawMessage(`{"message":"world"}`)
	h1 := ComputeRequestHash(body1, "POST", "/v1/workspaces/ws-1/snapshots")
	h2 := ComputeRequestHash(body2, "POST", "/v1/workspaces/ws-1/snapshots")
	if h1 == h2 {
		t.Fatal("different bodies produced same hash")
	}
}

func TestComputeRequestHash_DifferentMethod(t *testing.T) {
	body := json.RawMessage(`{"message":"hello"}`)
	h1 := ComputeRequestHash(body, "POST", "/v1/workspaces/ws-1/snapshots")
	h2 := ComputeRequestHash(body, "DELETE", "/v1/workspaces/ws-1/snapshots")
	if h1 == h2 {
		t.Fatal("different methods produced same hash")
	}
}
