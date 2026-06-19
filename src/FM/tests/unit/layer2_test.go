package layer1_test

import (
	"context"
	"testing"
	"time"

	"github.com/dashfabric/fm/pkg/dm"
)

// TC-L2-001: Create VnetRegistry
func TestVnetRegistry_Create(t *testing.T) {
	reg := datamanagement.NewVnetRegistry("vnet-001")
	if reg == nil {
		t.Fatal("registry should not be nil")
	}
}

// TC-L2-002: Add and Get ENI
func TestVnetRegistry_AddAndGetENI(t *testing.T) {
	reg := datamanagement.NewVnetRegistry("vnet-001")

	eni := &datamanagement.ENIState{
		ID:        "eni-001",
		VnetID:    "vnet-001",
		MAC:       "00:11:22:33:44:55",
		Status:    "active",
		IPAddress: "10.0.0.1",
	}

	reg.AddENI(eni)

	retrieved, ok := reg.GetENI("eni-001")
	if !ok {
		t.Fatal("ENI should be found")
	}
	if retrieved.ID != "eni-001" {
		t.Errorf("expected ID eni-001, got %s", retrieved.ID)
	}
}

// TC-L2-003: List ENIs
func TestVnetRegistry_ListENIs(t *testing.T) {
	reg := datamanagement.NewVnetRegistry("vnet-001")

	for i := 1; i <= 5; i++ {
		eni := &datamanagement.ENIState{
			ID:     "eni-" + string(rune('0'+i)),
			VnetID: "vnet-001",
			Status: "active",
		}
		reg.AddENI(eni)
	}

	enis := reg.ListENIs()
	if len(enis) != 5 {
		t.Errorf("expected 5 ENIs, got %d", len(enis))
	}
}

// TC-L2-004: NicRegistry VIP Bindings
func TestNicRegistry_Bindings(t *testing.T) {
	reg := datamanagement.NewNicRegistry("nic-001")

	binding := &datamanagement.VIPBinding{
		VIP:    "10.1.1.1",
		DIP:    "10.0.0.1",
		Status: "active",
	}

	reg.AddBinding("10.1.1.1", binding)

	retrieved, ok := reg.GetBinding("10.1.1.1")
	if !ok {
		t.Fatal("binding should be found")
	}
	if retrieved.VIP != "10.1.1.1" {
		t.Errorf("expected VIP 10.1.1.1, got %s", retrieved.VIP)
	}
}

// TC-L2-005: MappingManager Create
func TestMappingManager_Create(t *testing.T) {
	mm := datamanagement.NewMappingManager()
	if mm == nil {
		t.Fatal("manager should not be nil")
	}
}

// TC-L2-006: Register Consistency Rules
func TestMappingManager_RegisterRules(t *testing.T) {
	mm := datamanagement.NewMappingManager()

	mm.RegisterRule(&datamanagement.ENIStateRule{})
	mm.RegisterRule(&datamanagement.VIPBindingRule{})
	mm.RegisterRule(&datamanagement.SNATPoolRule{})
	mm.RegisterRule(&datamanagement.RouteValidityRule{})
	mm.RegisterRule(&datamanagement.ReplicaHealthRule{})

	// All 5 rules should be registered
	// (We can't directly check count, but Validate should work)
	ctx := context.Background()
	err := mm.ValidateConsistency(ctx)
	if err != nil {
		t.Errorf("validation should succeed with empty state: %v", err)
	}
}

// TC-L2-007: ENI State Validation - Valid States
func TestENIStateRule_ValidStates(t *testing.T) {
	rule := &datamanagement.ENIStateRule{}
	ctx := context.Background()

	state := &datamanagement.SystemState{
		ENIs:     make(map[string]*datamanagement.ENIState),
		VIPs:     make(map[string]*datamanagement.VIPBinding),
		Replicas: make(map[string]*datamanagement.ReplicaState),
		Metadata: make(map[string]interface{}),
	}

	// Test each valid state
	for _, status := range []string{"active", "inactive", "error"} {
		state.ENIs["test-eni"] = &datamanagement.ENIState{
			ID:     "test-eni",
			Status: status,
		}

		err := rule.Validate(ctx, state)
		if err != nil {
			t.Errorf("valid status %q should not error: %v", status, err)
		}
	}
}

// TC-L2-008: ENI State Validation - Invalid State
func TestENIStateRule_InvalidState(t *testing.T) {
	rule := &datamanagement.ENIStateRule{}
	ctx := context.Background()

	state := &datamanagement.SystemState{
		ENIs:     make(map[string]*datamanagement.ENIState),
		VIPs:     make(map[string]*datamanagement.VIPBinding),
		Replicas: make(map[string]*datamanagement.ReplicaState),
		Metadata: make(map[string]interface{}),
	}

	state.ENIs["test-eni"] = &datamanagement.ENIState{
		ID:     "test-eni",
		Status: "unknown",
	}

	err := rule.Validate(ctx, state)
	if err == nil {
		t.Error("invalid status should error")
	}
}

// TC-L2-009: VIP Binding Rule - Valid
func TestVIPBindingRule_Valid(t *testing.T) {
	rule := &datamanagement.VIPBindingRule{}
	ctx := context.Background()

	state := &datamanagement.SystemState{
		ENIs:     make(map[string]*datamanagement.ENIState),
		VIPs:     make(map[string]*datamanagement.VIPBinding),
		Replicas: make(map[string]*datamanagement.ReplicaState),
		Metadata: make(map[string]interface{}),
	}

	state.VIPs["10.1.1.1"] = &datamanagement.VIPBinding{
		VIP:    "10.1.1.1",
		DIP:    "10.0.0.1",
		Status: "active",
	}

	err := rule.Validate(ctx, state)
	if err != nil {
		t.Errorf("valid binding should not error: %v", err)
	}
}

// TC-L2-010: VIP Binding Rule - Missing DIP
func TestVIPBindingRule_MissingDIP(t *testing.T) {
	rule := &datamanagement.VIPBindingRule{}
	ctx := context.Background()

	state := &datamanagement.SystemState{
		ENIs:     make(map[string]*datamanagement.ENIState),
		VIPs:     make(map[string]*datamanagement.VIPBinding),
		Replicas: make(map[string]*datamanagement.ReplicaState),
		Metadata: make(map[string]interface{}),
	}

	state.VIPs["10.1.1.1"] = &datamanagement.VIPBinding{
		VIP:    "10.1.1.1",
		DIP:    "", // Missing!
		Status: "active",
	}

	err := rule.Validate(ctx, state)
	if err == nil {
		t.Error("missing DIP should error")
	}
}

// TC-L2-011: SNAT Pool Rule - Valid
func TestSNATPoolRule_Valid(t *testing.T) {
	rule := &datamanagement.SNATPoolRule{}
	ctx := context.Background()

	state := &datamanagement.SystemState{
		ENIs:     make(map[string]*datamanagement.ENIState),
		VIPs:     make(map[string]*datamanagement.VIPBinding),
		Replicas: make(map[string]*datamanagement.ReplicaState),
		Metadata: make(map[string]interface{}),
	}

	state.ENIs["eni-001"] = &datamanagement.ENIState{ID: "eni-001"}
	state.VIPs["10.1.1.1"] = &datamanagement.VIPBinding{
		VIP:    "10.1.1.1",
		DIP:    "10.0.0.1",
		SNAT:   true,
		ENI:    "eni-001",
		Status: "active",
	}

	err := rule.Validate(ctx, state)
	if err != nil {
		t.Errorf("valid SNAT should not error: %v", err)
	}
}

// TC-L2-012: SNAT Pool Rule - Missing ENI
func TestSNATPoolRule_MissingENI(t *testing.T) {
	rule := &datamanagement.SNATPoolRule{}
	ctx := context.Background()

	state := &datamanagement.SystemState{
		ENIs:     make(map[string]*datamanagement.ENIState),
		VIPs:     make(map[string]*datamanagement.VIPBinding),
		Replicas: make(map[string]*datamanagement.ReplicaState),
		Metadata: make(map[string]interface{}),
	}

	state.VIPs["10.1.1.1"] = &datamanagement.VIPBinding{
		VIP:    "10.1.1.1",
		DIP:    "10.0.0.1",
		SNAT:   true,
		ENI:    "", // Missing!
		Status: "active",
	}

	err := rule.Validate(ctx, state)
	if err == nil {
		t.Error("SNAT without ENI should error")
	}
}

// TC-L2-013: Actor Creation
func TestActor_Create(t *testing.T) {
	actor := datamanagement.NewActor("actor-001", "vnet")
	if actor.ID != "actor-001" {
		t.Errorf("expected ID actor-001, got %s", actor.ID)
	}
	if actor.Type != "vnet" {
		t.Errorf("expected type vnet, got %s", actor.Type)
	}
	if actor.Queue == nil {
		t.Error("actor queue should be initialized")
	}
}

// TC-L2-014: Actor Message Sending
func TestActor_SendMessage(t *testing.T) {
	actor := datamanagement.NewActor("actor-001", "vnet")

	msg := datamanagement.ActorMessage{
		Sender:  "test",
		ReplyTo: make(chan error, 1),
	}

	err := actor.SendMessage(msg)
	if err != nil {
		t.Errorf("send should succeed: %v", err)
	}

	// Verify message was queued
	select {
	case received := <-actor.Queue:
		if received.Sender != "test" {
			t.Errorf("expected sender test, got %s", received.Sender)
		}
	case <-time.After(1 * time.Second):
		t.Error("message should be received")
	}
}

// TC-L2-015: VnetRegistry Thread Safety
func TestVnetRegistry_ThreadSafety(t *testing.T) {
	reg := datamanagement.NewVnetRegistry("vnet-001")

	// Concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			eni := &datamanagement.ENIState{
				ID:     "eni-" + string(rune('0'+id)),
				VnetID: "vnet-001",
				Status: "active",
			}
			reg.AddENI(eni)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	enis := reg.ListENIs()
	if len(enis) != 10 {
		t.Errorf("expected 10 ENIs, got %d", len(enis))
	}
}

// TC-L2-016: Route Validity Rule - No Healthy Replicas
func TestRouteValidityRule_NoHealthyReplicas(t *testing.T) {
	rule := &datamanagement.RouteValidityRule{}
	ctx := context.Background()

	state := &datamanagement.SystemState{
		ENIs:     make(map[string]*datamanagement.ENIState),
		VIPs:     make(map[string]*datamanagement.VIPBinding),
		Replicas: make(map[string]*datamanagement.ReplicaState),
		Metadata: make(map[string]interface{}),
	}

	// Add ENI but no healthy replica
	state.ENIs["eni-001"] = &datamanagement.ENIState{ID: "eni-001"}
	state.Replicas["rep-001"] = &datamanagement.ReplicaState{
		ID:      "rep-001",
		Healthy: false,
	}

	err := rule.Validate(ctx, state)
	if err == nil {
		t.Error("no healthy replicas should error")
	}
}

// TC-L2-017: Replica Health Rule
func TestReplicaHealthRule_UnhealthyReplica(t *testing.T) {
	rule := &datamanagement.ReplicaHealthRule{}
	ctx := context.Background()

	state := &datamanagement.SystemState{
		ENIs:     make(map[string]*datamanagement.ENIState),
		VIPs:     make(map[string]*datamanagement.VIPBinding),
		Replicas: make(map[string]*datamanagement.ReplicaState),
		Metadata: make(map[string]interface{}),
	}

	state.Replicas["rep-001"] = &datamanagement.ReplicaState{
		ID:      "rep-001",
		Healthy: false,
	}

	// Should not error (warning only)
	err := rule.Validate(ctx, state)
	if err != nil {
		t.Errorf("unhealthy replica should not error: %v", err)
	}
}

// TC-L2-018: MappingManager GetOrCreate VnetRegistry
func TestMappingManager_GetOrCreateVnetRegistry(t *testing.T) {
	mm := datamanagement.NewMappingManager()

	reg1 := mm.GetOrCreateVnetRegistry("vnet-001")
	reg2 := mm.GetOrCreateVnetRegistry("vnet-001")

	if reg1 != reg2 {
		t.Error("should return same registry for same vnet")
	}
}

// TC-L2-019: MappingManager GetOrCreate NicRegistry
func TestMappingManager_GetOrCreateNicRegistry(t *testing.T) {
	mm := datamanagement.NewMappingManager()

	reg1 := mm.GetOrCreateNicRegistry("nic-001")
	reg2 := mm.GetOrCreateNicRegistry("nic-001")

	if reg1 != reg2 {
		t.Error("should return same registry for same nic")
	}
}

// TC-L2-020: Consistency Validation - All Rules Pass
func TestMappingManager_AllRulesPass(t *testing.T) {
	mm := datamanagement.NewMappingManager()
	mm.RegisterRule(&datamanagement.ENIStateRule{})
	mm.RegisterRule(&datamanagement.VIPBindingRule{})

	ctx := context.Background()
	err := mm.ValidateConsistency(ctx)
	if err != nil {
		t.Errorf("validation should pass: %v", err)
	}
}
