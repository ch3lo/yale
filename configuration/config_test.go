package configuration

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"gopkg.in/yaml.v2"
)

// configStruct is a canonical example configuration, which should map to configYaml
var configStruct = Configuration{
	Clusters: map[string]Cluster{
		"dal": Cluster{
			Scheduler: Scheduler{
				"swarm": Parameters{
					"address":   "1.1.1.1:2376",
					"tlsverify": true,
					"tlscacert": "ca-swarm.pem",
					"tlscert":   "cert-swarm.pem",
					"tlskey":    "key-swarm.pem",
					"authfile":  ".dockercfg",
				},
			},
		},
		"wdc": Cluster{
			Scheduler: Scheduler{
				"swarm": Parameters{
					"address":   "2.2.2.2:2376",
					"tlsverify": true,
					"tlscacert": "ca-swarm.pem",
					"tlscert":   "cert-swarm.pem",
					"tlskey":    "key-swarm.pem",
				},
			},
		},
		"sjc": Cluster{
			Disabled: true,
			Scheduler: Scheduler{
				"marathon": Parameters{
					"address":   "3.3.3.3:8081",
					"tlsverify": true,
					"tlscacert": "ca-marathon.pem",
					"tlscert":   "cert-marathon.pem",
					"tlskey":    "key-marathon.pem",
				},
			},
		},
	},
}

// configYaml document representing configStruct
var configYaml = `
cluster:
  dal:
    scheduler:
      swarm:
        address: 1.1.1.1:2376
        tlsverify: true
        tlscacert: ca-swarm.pem
        tlscert: cert-swarm.pem
        tlskey: key-swarm.pem
		authfile: .dockercfg
  wdc:
    scheduler:
      swarm:
        address: 2.2.2.2:2376
        tlsverify: true
        tlscacert: ca-swarm.pem
        tlscert: cert-swarm.pem
        tlskey: key-swarm.pem
  sjc:
    disabled: true
    scheduler:
      marathon:
        address: 3.3.3.3:8081
        tlsverify: true
        tlscacert: ca-marathon.pem
        tlscert: cert-marathon.pem
        tlskey: key-marathon.pem
`

func Test(t *testing.T) {
	suite.Run(t, new(ConfigSuite))
}

type ConfigSuite struct {
	suite.Suite
	expectedConfig Configuration
}

func (suite *ConfigSuite) SetupTest() {
	os.Clearenv()
	suite.expectedConfig = configStruct
}

func (suite *ConfigSuite) TestMarshalRoundtrip() {
	configBytes, err := yaml.Marshal(suite.expectedConfig)
	assert.Nil(suite.T(), err)
	var config Configuration
	err = yaml.Unmarshal(configBytes, &config)
	assert.Nil(suite.T(), err)
	assert.True(suite.T(), assert.ObjectsAreEqual(config, suite.expectedConfig))
}

func (suite *ConfigSuite) TestParseSimple() {
	var config Configuration
	err := yaml.Unmarshal([]byte(configYaml), &config)
	assert.Nil(suite.T(), err)
	assert.True(suite.T(), assert.ObjectsAreEqual(config, suite.expectedConfig))
}
