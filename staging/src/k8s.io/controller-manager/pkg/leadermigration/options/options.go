/*
 Copyright 2021 The Kubernetes Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package options

import (
	"fmt"
	"github.com/spf13/pflag"
	"k8s.io/controller-manager/config"
	migrationconfig "k8s.io/controller-manager/pkg/leadermigration/config"
)

type LeaderMigrationOptions struct {
	Enabled                   bool
	ControllerMigrationConfig string
}

func (o *LeaderMigrationOptions) AddFlags(fs *pflag.FlagSet) {
	if o == nil {
		return
	}
	fs.BoolVar(&o.Enabled, "enable-leader-migration", false, "Whether to enable controller leader migration.")
	fs.StringVar(&o.ControllerMigrationConfig, "leader-migration-config", "", "Path to the config file for controller leader migration. Leave empty to use default value.")
}

func (o *LeaderMigrationOptions) ApplyTo(cfg *config.GenericControllerManagerConfiguration) []error {
	if o == nil {
		return nil
	}
	cfg.LeaderMigrationEnabled = o.Enabled
	if o.ControllerMigrationConfig == "" {
		cfg.LeaderMigrationConfiguration = migrationconfig.DefaultLeaderMigrationConfiguration
		return nil
	}
	leaderMigrationConfig, err := migrationconfig.ReadLeaderMigrationConfiguration(o.ControllerMigrationConfig)
	if err != nil {
		return []error{err}
	}
	errs := migrationconfig.ValidateLeaderMigrationConfiguration(leaderMigrationConfig)
	if len(errs) != 0 {
		return []error{fmt.Errorf("failed to parse leader migration configuration: %v", errs)}
	}
	cfg.LeaderMigrationConfiguration = *leaderMigrationConfig
	return nil
}
