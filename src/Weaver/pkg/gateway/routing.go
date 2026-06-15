package gateway

import (
	"sync/atomic"
)


// GetLoadDistribution returns the current load distribution across replicas
func (gw *Gateway) GetLoadDistribution() map[string]int64 {
	distribution := make(map[string]int64)
	gw.replicasMu.RLock()
	defer gw.replicasMu.RUnlock()

	for _, replica := range gw.replicas {
		distribution[replica.Name] = atomic.LoadInt64(&replica.ActiveConnections)
	}
	return distribution
}
