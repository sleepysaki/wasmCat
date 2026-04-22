package master

import (
	"errors"
	"math"

	"wasmcat/internal/shared"
)

// EarthRadius is the mean radius of the Earth in kilometers.
const EarthRadius = 6371.0

// FindClosestWorker iterates through all active workers and returns the one physically closest to the user.
func FindClosestWorker(userLat, userLon float64, workers []shared.WorkerNode) (shared.WorkerNode, error) {
	if len(workers) == 0 {
		return shared.WorkerNode{}, errors.New("no active workers available in the cluster")
	}

	var closestWorker shared.WorkerNode
	shortestDistance := math.MaxFloat64 // Start with the highest possible number

	for _, worker := range workers {
		// Calculate the physical distance in kilometers
		distance := calculateHaversine(userLat, userLon, worker.Latitude, worker.Longitude)

		// If this worker is closer than the current record, update the winner
		if distance < shortestDistance {
			shortestDistance = distance
			closestWorker = worker
		}
	}

	return closestWorker, nil
}

// calculateHaversine computes the great-circle distance between two points on a sphere.
// It converts GPS decimal degrees to radians to perform the spherical trigonometry.
func calculateHaversine(lat1, lon1, lat2, lon2 float64) float64 {
	// Convert decimal degrees to radians
	lat1Rad := degreesToRadians(lat1)
	lon1Rad := degreesToRadians(lon1)
	lat2Rad := degreesToRadians(lat2)
	lon2Rad := degreesToRadians(lon2)

	// Calculate the differences
	deltaLat := lat2Rad - lat1Rad
	deltaLon := lon2Rad - lon1Rad

	// Apply the Haversine formula
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2)*math.Sin(deltaLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	// Return distance in kilometers
	return EarthRadius * c
}

// degreesToRadians is a simple helper to convert degrees to radians.
func degreesToRadians(degrees float64) float64 {
	return degrees * (math.Pi / 180.0)
}