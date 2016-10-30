package core

// SetMetadata will set metadata
func (conf *BuildConfig) SetMetadata(key, value string) {
	conf.m.Lock()
	defer conf.m.Unlock()

	if conf.metadata == nil {
		conf.metadata = make(map[string]string)
	}

	conf.metadata[key] = value
}

// GetMetadata will get metadata
func (conf *BuildConfig) GetMetadata(key string) string {
	conf.m.RLock()
	defer conf.m.RUnlock()

	if conf.metadata == nil {
		return ""
	}

	return conf.metadata[key]
}
