package core

import (
	"encoding/json"
	"io/ioutil"
)

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

type marshalledBuildConfig struct {
	Config   *BuildConfig
	Metadata *map[string]string
}

// Marshal will marshall this structure into a string
func (conf *BuildConfig) Marshal() ([]byte, error) {
	conf.m.RLock()
	conf.m.RUnlock()

	marshalledConf := marshalledBuildConfig{
		Config:   conf,
		Metadata: &conf.metadata,
	}

	marshalled, err := json.MarshalIndent(&marshalledConf, "", "    ")
	if err != nil {
		return nil, err
	}

	return marshalled, nil
}

// UnmarshalBuildConfig will unmarshall the given filename into a BuildConfig
func UnmarshalBuildConfig(filename string) (*BuildConfig, error) {
	marshalledConf := marshalledBuildConfig{}

	if data, err := ioutil.ReadFile(filename); err != nil {
		return nil, err
	} else if err := json.Unmarshal(data, &marshalledConf); err != nil {
		return nil, err
	}
	conf := marshalledConf.Config
	conf.metadata = make(map[string]string)
	// there isn't a nice way of copying a map in go.. so here we go
	for key, value := range *marshalledConf.Metadata {
		conf.metadata[key] = value
	}
	return conf, nil
}
