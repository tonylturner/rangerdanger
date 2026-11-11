package server

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Only shipped, immutable definitions are cached. Tests of loading, validation
// and reseeding still use the real loader and their own directories/databases.
var generationFixtures = map[string]*generationFixture{
	usPackageID: {},
	euPackageID: {},
}

type generationFixture struct {
	once sync.Once
	pkg  labs.Package
	man  *manifest.Manifest
	err  error
}

func packageGenerationFixture(id string) (labs.Package, *manifest.Manifest, error) {
	fixture := generationFixtures[id]
	fixture.once.Do(func() {
		fixture.man, fixture.err = manifest.Load(repoRoot(), id)
		if fixture.err != nil {
			return
		}
		fixture.pkg, fixture.err = labs.ReadDefinition(
			filepath.Join(repoRoot(), "lab-definitions"),
			filepath.Join(manifest.Dir(repoRoot(), id), "package.yml"))
	})
	if fixture.err != nil {
		return labs.Package{}, nil, fixture.err
	}
	return copyGenerationFixture(reflect.ValueOf(fixture.pkg)).Interface().(labs.Package),
		copyGenerationFixture(reflect.ValueOf(fixture.man)).Interface().(*manifest.Manifest), nil
}

// These DTOs contain slices, maps, pointers and interface-valued expectations.
// JSON would omit Curriculum.Plan and KnowledgeArticle.File, and lose the
// private policy envelope. Preserve that envelope as read-only: labs only reads
// it in CompilePlan, and server tests cannot access it. Reject new private
// fields or non-data types rather than silently sharing mutable state.
func copyGenerationFixture(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
	}
	switch value.Kind() {
	case reflect.Pointer:
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(copyGenerationFixture(value.Elem()))
		return out
	case reflect.Interface:
		out := reflect.New(value.Type()).Elem()
		out.Set(copyGenerationFixture(value.Elem()))
		return out
	case reflect.Slice:
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(copyGenerationFixture(value.Index(i)))
		}
		return out
	case reflect.Map:
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			out.SetMapIndex(copyGenerationFixture(iter.Key()), copyGenerationFixture(iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if !field.IsExported() {
				if value.Type() != reflect.TypeFor[labs.Package]() || field.Name != "planEnvelope" {
					panic("unexpected private fixture field: " + field.Name)
				}
				continue
			}
			out.Field(i).Set(copyGenerationFixture(value.Field(i)))
		}
		return out
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return value
	default:
		panic("unexpected fixture type: " + value.Type().String())
	}
}

func TestGenerationFixtureIsolation(t *testing.T) {
	for _, id := range []string{usPackageID, euPackageID} {
		t.Run(id, func(t *testing.T) {
			first, firstManifest, err := packageGenerationFixture(id)
			if err != nil {
				t.Fatal(err)
			}
			second, secondManifest, err := packageGenerationFixture(id)
			if err != nil {
				t.Fatal(err)
			}
			source := generationFixtures[id]
			if !reflect.DeepEqual(first, source.pkg) || !reflect.DeepEqual(firstManifest, source.man) ||
				!reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstManifest, secondManifest) {
				t.Fatal("fixture copies differ from the loaded source")
			}
			assertFixtureReferencesDistinct(t, reflect.ValueOf(first), reflect.ValueOf(source.pkg))
			assertFixtureReferencesDistinct(t, reflect.ValueOf(firstManifest), reflect.ValueOf(source.man))
			assertFixtureReferencesDistinct(t, reflect.ValueOf(first), reflect.ValueOf(second))
			assertFixtureReferencesDistinct(t, reflect.ValueOf(firstManifest), reflect.ValueOf(secondManifest))
		})
	}
}

// Check every reference, including deeply nested curriculum/plan collections
// and interface-valued expectations, not just fields currently mutated by tests.
func assertFixtureReferencesDistinct(t *testing.T, first, second reflect.Value) {
	t.Helper()
	switch first.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if first.IsNil() {
			return
		}
		if (first.Kind() != reflect.Slice || first.Len() > 0) && first.Pointer() == second.Pointer() {
			t.Fatalf("fixture copies share %s", first.Type())
		}
	}
	switch first.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !first.IsNil() {
			assertFixtureReferencesDistinct(t, first.Elem(), second.Elem())
		}
	case reflect.Struct:
		for i := 0; i < first.NumField(); i++ {
			if first.Type().Field(i).IsExported() {
				assertFixtureReferencesDistinct(t, first.Field(i), second.Field(i))
			}
		}
	case reflect.Slice:
		for i := 0; i < first.Len(); i++ {
			assertFixtureReferencesDistinct(t, first.Index(i), second.Index(i))
		}
	case reflect.Map:
		iter := first.MapRange()
		for iter.Next() {
			assertFixtureReferencesDistinct(t, iter.Value(), second.MapIndex(iter.Key()))
		}
	}
}
