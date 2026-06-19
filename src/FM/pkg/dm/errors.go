package datamanagement

import "fmt"

var (
	ErrActorQueueFull        = fmt.Errorf("actor queue full")
	ErrENINotFound           = fmt.Errorf("eni not found")
	ErrVIPNotFound           = fmt.Errorf("vip not found")
	ErrReplicaNotFound       = fmt.Errorf("replica not found")
	ErrConsistencyViolation  = fmt.Errorf("consistency rule violated")
	ErrInvalidState          = fmt.Errorf("invalid state")
	ErrStateTransitionFailed = fmt.Errorf("state transition failed")
)

// ConsistencyViolationError provides details about which rule was violated
type ConsistencyViolationError struct {
	Rule    string
	Details string
}

func (e *ConsistencyViolationError) Error() string {
	return fmt.Sprintf("consistency rule '%s' violated: %s", e.Rule, e.Details)
}
