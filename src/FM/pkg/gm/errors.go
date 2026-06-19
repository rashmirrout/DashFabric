package goalstatemanagement

import "fmt"

// ErrAggregationFailed indicates aggregation failure
var ErrAggregationFailed = fmt.Errorf("goal state aggregation failed")

// ErrGenerationFailed indicates generation failure
var ErrGenerationFailed = fmt.Errorf("goal state generation failed")

// ErrNoHealthyReplicas indicates no healthy replicas available
var ErrNoHealthyReplicas = fmt.Errorf("no healthy replicas available")

// ErrCacheError indicates cache operation error
var ErrCacheError = fmt.Errorf("goal state cache error")
