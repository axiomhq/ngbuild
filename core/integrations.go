package core

import "sync"

var globalIntegrationsCacheOnce sync.Once
var globalIntegrationsCache []Integration

// SetIntegrations will set the integrations
func SetIntegrations(integrations []Integration) {
	globalIntegrationsCacheOnce.Do(func() {
		globalIntegrationsCache = integrations
	})

}

func getIndexOf(slice []Integration, search string) int {
	for i, val := range slice {
		if val.Identifier() == search {
			return i
		}
	}

	return -1
}

// GetIntegrations will return a list of cached Integration variables
// anything passed in to disabledIntegrations will be removed from the cache
func GetIntegrations(disabledIntegrations ...string) []Integration {

	integrations := globalIntegrationsCache[:]
	for _, disabledIntegration := range disabledIntegrations {
		indexOfDisabledIntegration := getIndexOf(integrations, disabledIntegration)
		if indexOfDisabledIntegration < 0 {
			continue
		}

		integrations = append(integrations[:indexOfDisabledIntegration], integrations[indexOfDisabledIntegration+1:]...)
	}

	return integrations
}
