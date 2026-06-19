package dpuabstraction

import "fmt"

// ErrProgrammingFailed indicates programming failure
var ErrProgrammingFailed = fmt.Errorf("device programming failed")

// ErrPluginNotFound indicates requested plugin not available
var ErrPluginNotFound = fmt.Errorf("plugin not found")

// ErrPluginLoadFailed indicates plugin loading failure
var ErrPluginLoadFailed = fmt.Errorf("plugin load failed")

// ErrDispatchError indicates dispatcher error
var ErrDispatchError = fmt.Errorf("dispatch error")

// ErrPoolError indicates worker pool error
var ErrPoolError = fmt.Errorf("worker pool error")
