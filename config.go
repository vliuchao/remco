/*
 * This file is part of remco.
 * © 2016 The Remco Authors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package main

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HeavyHorst/remco/backends"
	backendErrors "github.com/HeavyHorst/remco/backends/error"
	"github.com/HeavyHorst/remco/log"
	"github.com/HeavyHorst/remco/template"
	"github.com/Sirupsen/logrus"
	"github.com/naoina/toml"
)

type resource struct {
	Template []*template.ProcessConfig
	Backend  backends.Config
}

// configuration is the representation of an config file
type configuration struct {
	LogLevel   string `toml:"log_level"`
	LogFormat  string `toml:"log_format"`
	IncludeDir string `toml:"include_dir"`
	Resource   []resource
}

func readFileAndExpandEnv(path string) ([]byte, error) {
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		return buf, err
	}
	// expand the environment variables
	buf = []byte(os.ExpandEnv(string(buf)))
	return buf, nil
}

// newConfiguration reads the file at `path`, expand the environment variables
// and unmarshals it to a new configuration struct.
// it returns an error if any.
func newConfiguration(path string) (configuration, error) {
	var c configuration

	buf, err := readFileAndExpandEnv(path)
	if err != nil {
		return c, err
	}

	if err := toml.Unmarshal(buf, &c); err != nil {
		return c, err
	}

	c.configureLogger()

	if c.IncludeDir != "" {
		files, err := ioutil.ReadDir(c.IncludeDir)
		if err != nil {
			return c, err
		}
		for _, file := range files {
			if strings.HasSuffix(file.Name(), ".toml") {
				fp := filepath.Join(c.IncludeDir, file.Name())
				log.WithFields(logrus.Fields{
					"path": fp,
				}).Info("Loading resource configuration")
				buf, err := readFileAndExpandEnv(fp)
				if err != nil {
					return c, err
				}
				var r resource
				if err := toml.Unmarshal(buf, &r); err != nil {
					return c, err
				}
				// don't add empty resources
				if len(r.Template) > 0 {
					c.Resource = append(c.Resource, r)
				}
			}
		}
	}

	return c, nil
}

// configureLogger configures the global logger
// for example it sets the log level and log formatting
func (c *configuration) configureLogger() {
	if c.LogLevel != "" {
		err := log.SetLevel(c.LogLevel)
		if err != nil {
			log.Error(err)
		}
	}
	if c.LogFormat != "" {
		log.SetFormatter(c.LogFormat)
	}
}

// run connects to all given backends and starts the template processing as defined in the config file
func (c *configuration) run(stop chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})

	wait := sync.WaitGroup{}
	for _, v := range c.Resource {
		wait.Add(1)
		go func(v resource) {
			var backendList template.Backends
			defer wait.Done()

			// try to connect to all backends
			// connection to all backends must succeed to continue
			for _, config := range v.Backend.GetBackends() {
			retryloop:
				for {
					select {
					case <-ctx.Done():
						backendList.Close()
						return
					default:
						b, err := config.Connect()
						if err == nil {
							backendList = append(backendList, b)
						} else if err != backendErrors.ErrNilConfig {
							log.WithFields(logrus.Fields{
								"backend": b.Name,
							}).Error(err)
							// try again every 2 seconds
							time.Sleep(2 * time.Second)
							continue retryloop
						}
						// break out of the loop on success or if the backend is nil
						break retryloop
					}
				}
			}

			// make sure that all backend clients are closed cleanly
			defer backendList.Close()

			t, err := template.NewResource(backendList, v.Template)
			if err != nil {
				log.Error(err.Error())
				return
			}

			t.Monitor(ctx)
		}(v)
	}

	go func() {
		// If there is no goroutine left - quit
		wait.Wait()
		close(done)
	}()

	for {
		select {
		case <-stop:
			cancel()
			wait.Wait()
			return
		case <-done:
			return
		}
	}
}
