package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const imageModulesDir = "/usr/lib/modules/"

type alias struct{ pattern, module string } // module alias info

var (
	loadedModules  = make(map[string]bool)
	loadingModules = make(map[string][]*sync.WaitGroup)
	modulesMutex   sync.Mutex
)

func loadModalias(alias string) error {
	mods, err := matchAlias(alias)
	if err != nil {
		debug("unable to match modalias %s: %v", alias, err)
		return nil
	}
	if len(mods) == 0 {
		return fmt.Errorf("no match found for alias %s", alias)
	}
	_ = loadModules(mods...)
	return nil
}

var aliases []alias

func readAliases() error {
	f, err := os.Open(imageModulesDir + "/booster.alias")
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		parts := strings.Split(line, " ")
		aliases = append(aliases, alias{parts[0], parts[1]})
	}

	return s.Err()
}

func loadModuleUnlocked(wg *sync.WaitGroup, modules ...string) {
	for _, module := range modules {
		if _, ok := loadedModules[module]; ok {
			continue // the module is already loaded
		}

		_, alreadyLoading := loadingModules[module]
		wg.Add(1)
		loadingModules[module] = append(loadingModules[module], wg)

		if alreadyLoading {
			// we already incremented 'wg' counter
			// now wait till the module is loaded
			continue
		}

		var depsWg sync.WaitGroup
		if deps, ok := config.ModuleDependencies[module]; ok {
			loadModuleUnlocked(&depsWg, deps...)
		}

		// pay attention that 'module' is a loop variable and cannot be passed to goroutine
		// https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		mod := module
		go func() {
			depsWg.Wait()
			debug("loading module %s", mod)
			if err := finitModule(mod); err != nil {
				fmt.Println(err)
				return
			}

			modulesMutex.Lock()
			defer modulesMutex.Unlock()

			for _, w := range loadingModules[mod] {
				// signal waiters that the module is loaded
				w.Done()
			}
			delete(loadingModules, mod)
			loadedModules[mod] = true
		}()
	}
}

func finitModule(module string) error {
	f, err := os.Open(imageModulesDir + "/" + module + ".ko")
	if err != nil {
		return err
	}
	defer f.Close()

	if err := unix.FinitModule(int(f.Fd()), "", 0); err != nil {
		return fmt.Errorf("finit(%v): %v", module, err)
	}

	return nil
}

func loadModules(modules ...string) *sync.WaitGroup {
	var wg sync.WaitGroup

	modulesMutex.Lock()
	defer modulesMutex.Unlock()

	loadModuleUnlocked(&wg, modules...)
	return &wg
}

// returns all module names that match given alias
func matchAlias(alias string) ([]string, error) {
	var result []string
	for _, a := range aliases {
		match, err := path.Match(a.pattern, alias)
		if err != nil {
			return nil, err
		}
		if match {
			debug("modalias %v matched module %v", alias, a.module)
			result = append(result, a.module)
		}
	}
	return result, nil
}
