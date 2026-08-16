package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"golang.org/x/tools/go/packages"
)

// makeTwoPackageModule builds a module whose main package imports a second
// package in the same module and calls through an interface, so the load
// produces a real dependency package AND populates Uses/Defs/Selections.
func makeTwoPackageModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", "module example.com/testmod\n\ngo 1.21\n")
	write("greet/greet.go", `package greet

// Greeter defines a greeting interface.
type Greeter interface {
	Greet(name string) string
}

// Simple implements Greeter.
type Simple struct {
	Prefix string
}

// Greet returns a greeting string.
func (g *Simple) Greet(name string) string {
	return g.Prefix + name
}
`)
	write("main.go", `package main

import (
	"strings"

	"example.com/testmod/greet"
)

func main() {
	var g greet.Greeter = &greet.Simple{Prefix: "hi "}
	_ = strings.TrimSpace(g.Greet("world"))
}
`)

	return dir
}

// The maps the resolver reads must survive, and the ones nothing reads must be
// released. Pinning the KEPT set is the point: a dropped map reads as empty
// rather than panicking, so a future caller that starts reading Types would get
// a silent wrong answer, and only this test would notice.
//
// Mutation that must turn it RED: delete the releaseUnreadTypeInfo(pkgs) call
// from LoadPackages.
func TestLoadPackages_ReleasesUnreadTypeInfoMaps(t *testing.T) {
	dir := makeTwoPackageModule(t)

	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(result.Packages) == 0 {
		t.Fatal("expected at least one package")
	}

	pkg := result.Packages[0]
	if pkg.TypesInfo == nil {
		t.Fatal("expected TypesInfo to be populated")
	}

	// Kept — the resolver reads these.
	if len(pkg.TypesInfo.Defs) == 0 {
		t.Error("Defs must survive: the resolver reads it")
	}
	if len(pkg.TypesInfo.Uses) == 0 {
		t.Error("Uses must survive: the resolver reads it")
	}

	// Released — nothing reads these, and Types is the expensive one.
	if pkg.TypesInfo.Types != nil {
		t.Errorf("Types must be released, got %d entries", len(pkg.TypesInfo.Types))
	}
	if pkg.TypesInfo.Scopes != nil {
		t.Errorf("Scopes must be released, got %d entries", len(pkg.TypesInfo.Scopes))
	}
	if pkg.TypesInfo.Implicits != nil {
		t.Errorf("Implicits must be released, got %d entries", len(pkg.TypesInfo.Implicits))
	}
	if pkg.TypesInfo.Instances != nil {
		t.Errorf("Instances must be released, got %d entries", len(pkg.TypesInfo.Instances))
	}
}

// The release must reach DEPENDENCY packages, not just the roots.
//
// This is where the memory actually is: NeedDeps gives every dependency its own
// fully-populated types.Info, and the roots are a small fraction of the total.
// A roots-only loop passes the test above while freeing almost nothing, so the
// saving would look delivered and not be.
//
// Mutation that must turn it RED: replace the packages.Visit walk in
// releaseUnreadTypeInfo with `for _, p := range pkgs`.
func TestLoadPackages_ReleasesAcrossDependencyGraph(t *testing.T) {
	dir := makeTwoPackageModule(t)

	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// "strings" is a STDLIB dependency: NeedDeps loads it with its own
	// types.Info, but the "./..." pattern never makes it a root. That is what
	// separates a graph walk from a roots-only loop — the module's own "greet"
	// package would NOT, because "./..." returns it as a root too, and a
	// roots-only loop cleans it just fine.
	var dep *packages.Package
	for _, root := range result.Packages {
		if imp, ok := root.Imports["strings"]; ok {
			dep = imp
		}
	}
	if dep == nil {
		t.Fatal("fixture is inert: the stdlib dependency was not loaded, so this " +
			"test cannot tell a graph walk from a roots-only loop")
	}
	if dep.TypesInfo == nil {
		t.Fatal("fixture is inert: the dependency carries no TypesInfo to release")
	}

	if len(dep.TypesInfo.Defs) == 0 {
		t.Error("the dependency's Defs must survive: the resolver reads it")
	}
	if dep.TypesInfo.Types != nil {
		t.Errorf("the dependency's Types must be released too, got %d entries — "+
			"a roots-only walk frees almost nothing", len(dep.TypesInfo.Types))
	}
}
