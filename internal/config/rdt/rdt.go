package rdt

import (
	"io/ioutil"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"

	"github.com/intel/goresctrl/pkg/rdt"
)

const (
	// DefaultRdtConfigFile is the default value for RDT config file path
	DefaultRdtConfigFile = ""
	// ResctrlPrefix is the prefix used for class/closid directories under the resctrl filesystem
	ResctrlPrefix = ""
)

type Config struct {
	enabled bool
	config  *rdt.Config
}

// New creates a new RDT config instance
func New() *Config {
	c := &Config{
		enabled: true,
		config:  &rdt.Config{},
	}

	rdt.SetLogger(logrus.StandardLogger())

	if err := rdt.Initialize(ResctrlPrefix); err != nil {
		logrus.Infof("RDT is not enabled: %v", err)
		c.enabled = false
	}
	return c
}

// Enabled returns true if RDT is enabled in the system
func (c *Config) Enabled() bool {
	return c.enabled
}

// Load loads and validates RDT config
func (c *Config) Load(path string) error {
	if !c.Enabled() {
		logrus.Info("RDT is disabled")
		return nil
	}

	if path == "" {
		logrus.Info("No RDT config file specified, RDT not configured")
		return nil
	}

	tmpCfg, err := loadConfigFile(path)
	if err != nil {
		return err
	}

	if err := rdt.SetConfig(tmpCfg, true); err != nil {
		return errors.Wrap(err, "configuring RDT failed")
	}

	logrus.Infof("RDT config successfully loaded from %q", path)
	c.config = tmpCfg

	return nil
}

func (c *Config) Apply() error {
	if err := rdt.SetConfig(c.config, true); err != nil {
		return errors.Wrap(err, "configuring RDT failed")
	}
	logrus.Infof("RDT successfully configured")
	return nil
}

func loadConfigFile(path string) (*rdt.Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "reading rdt config file failed")
	}

	c := &rdt.Config{}
	if err = yaml.Unmarshal(data, c); err != nil {
		return nil, errors.Wrap(err, "parsing RDT config failed")
	}

	return c, nil
}
