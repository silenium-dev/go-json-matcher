package matcher

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type Conflict struct {
	Path     string      `json:"path,omitempty"`
	Expected interface{} `json:"expected,omitempty"`
	Actual   interface{} `json:"actual,omitempty"`
	Error    error       `json:"error,omitempty"`
}

type matcher func(string, interface{}, interface{}) ([]Conflict, error)

//nolint:gochecknoglobals // an internal global here is more efficient than repeatedly creating the map in a hot path
var matchers map[reflect.Kind]matcher

//nolint:gochecknoinits // the assigned functions refer to `matchers` so we can't assign it directly: we need init()
func init() {
	matchers = map[reflect.Kind]matcher{
		reflect.Bool:    _matchPrimitive,
		reflect.String:  _matchPrimitive,
		reflect.Int:     _matchPrimitive,
		reflect.Int64:   _matchPrimitive,
		reflect.Float64: _matchPrimitive,
		reflect.Map:     _matchMap,
		reflect.Slice:   _matchSlice,
		reflect.Array:   _matchSlice,
	}
}

// JSONMatches checks if the JSON in `j` provided with the first argument
// satisfies the pattern in the second argument.
// Both `j` and `jPatternSpecifier` are passed as byte slices.
// The pattern can be a valid literal value (in that case an exact match will
// be required), a special marker (a string starting with the hash character
// '#'), or any combination of these via arrays and objects.
func JSONMatches(j []byte, jPatternSpecifier []byte) ([]Conflict, error) {
	var jAny interface{}
	err := json.Unmarshal(j, &jAny)
	if err != nil {
		return []Conflict{{
			Path:  "/",
			Error: err,
		}}, fmt.Errorf("can't unmarshal left argument: %w", err)
	}

	var patternSpecAny interface{}
	err = json.Unmarshal(jPatternSpecifier, &patternSpecAny)
	if err != nil {
		return []Conflict{{
			Path:  "/",
			Error: err,
		}}, fmt.Errorf("can't unmarshal pattern argument: %w", err)
	}

	conflicts, err := _match("/", jAny, patternSpecAny)
	var resultingConflicts []Conflict
	for _, c := range conflicts {
		if strings.HasPrefix(c.Path, "//") {
			c.Path = c.Path[1:]
		}
		resultingConflicts = append(resultingConflicts, c)
	}
	return resultingConflicts, err
}

// JSONStringMatches checks if the JSON string `j` provided with the first argument
// satisfies the pattern in the second argument.
// Both `j` and `jPatternSpecifier` are passed as strings.
// The pattern can be a valid literal value (in that case an exact match will
// be required), a special marker (a string starting with the hash character
// '#'), or any combination of these via arrays and objects.
func JSONStringMatches(j string, jPatternSpecifier string) ([]Conflict, error) {
	return JSONMatches([]byte(j), []byte(jPatternSpecifier))
}

func _matchZero(path string, x interface{}) []Conflict {
	xV := reflect.ValueOf(x)
	if !xV.IsValid() {
		return []Conflict{}
	}
	return []Conflict{
		{
			Path:     path,
			Expected: nil,
			Actual:   x,
		},
	}
}

var uuidRe = regexp.MustCompile(`(?i)^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var uuidV4Re = regexp.MustCompile(`(?i)^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89aAbB][a-f0-9]{3}-[a-f0-9]{12}$`)

const (
	ignoreMarker  = "#ignore"
	nullMarker    = "#null"
	presentMarker = "#present"
)

//nolint:funlen,gocognit // reducing the number of statements would reduce legibility in this instance
func _matchWithMarker(path string, x interface{}, marker string) ([]Conflict, error) {
	possibleConflict := Conflict{
		Path:     path,
		Expected: marker,
		Actual:   x,
	}
	if x == nil && (marker == ignoreMarker || marker == nullMarker || marker == presentMarker) {
		return []Conflict{}, nil
	}
	xV := reflect.ValueOf(x)
	if !xV.IsValid() {
		return []Conflict{possibleConflict}, nil // here we now that ref is non-zero
	}

	//nolint:gomnd // the "magic" literal constant 2 here is clearer than a synthetic constant symbol
	markerParts := strings.SplitN(marker, " ", 2)

	switch markerParts[0] {
	case ignoreMarker:
		return []Conflict{}, nil
	case nullMarker:
		if xV.Kind() != reflect.Ptr {
			return []Conflict{possibleConflict}, nil
		}
		if xV.IsNil() {
			return []Conflict{}, nil
		} else {
			return []Conflict{possibleConflict}, nil
		}
	case "#notnull":
		if xV.Kind() != reflect.Ptr {
			return []Conflict{}, nil
		}
		if !xV.IsNil() {
			return []Conflict{}, nil
		} else {
			return []Conflict{possibleConflict}, nil
		}
	case presentMarker:
		return []Conflict{}, nil
	case "#notpresent":
		return []Conflict{possibleConflict}, nil
	case "#array":
		if (xV.Kind() != reflect.Array) && (xV.Kind() != reflect.Slice) {
			return []Conflict{possibleConflict}, nil
		}
		return []Conflict{}, nil
	case "#object":
		if xV.Kind() != reflect.Map {
			return []Conflict{possibleConflict}, nil
		}
		return []Conflict{}, nil
	case "#bool":
		fallthrough
	case "#boolean":
		if xV.Kind() != reflect.Bool {
			return []Conflict{possibleConflict}, nil
		}
		return []Conflict{}, nil
	case "#number":
		if (xV.Kind() != reflect.Int64) && (xV.Kind() != reflect.Float64) {
			return []Conflict{possibleConflict}, nil
		}
		return []Conflict{}, nil
	case "#string":
		if xV.Kind() != reflect.String {
			return []Conflict{possibleConflict}, nil
		}
		return []Conflict{}, nil
	case "#date":
		if xV.Kind() == reflect.String {
			xString, ok := x.(string)
			if !ok {
				return []Conflict{possibleConflict}, nil
			}
			_, err := time.Parse("2006-01-02", xString)
			if err == nil {
				return []Conflict{}, nil
			}
			return []Conflict{possibleConflict}, nil
		} else if xV.Kind() == reflect.Struct {
			_, ok := x.(time.Time)
			if ok {
				return []Conflict{}, nil
			}
		}
		return []Conflict{possibleConflict}, nil

	case "#datetime":
		if xV.Kind() == reflect.String {
			xString, ok := x.(string)
			if !ok {
				return []Conflict{possibleConflict}, nil
			}
			_, err := time.Parse(time.RFC3339, xString)
			if err == nil {
				return []Conflict{}, nil
			}
			return []Conflict{possibleConflict}, nil
		} else if xV.Kind() == reflect.Struct {
			_, ok := x.(time.Time)
			if ok {
				return []Conflict{}, nil
			}
		}
		return []Conflict{possibleConflict}, nil
	case "#uuid":
		if xV.Kind() != reflect.String {
			return []Conflict{possibleConflict}, nil
		}
		xString, ok := x.(string)
		if ok {
			if uuidRe.MatchString(xString) {
				return []Conflict{}, nil
			}
		}
		return []Conflict{possibleConflict}, nil
	case "#uuid-v4":
		if xV.Kind() != reflect.String {
			return []Conflict{possibleConflict}, nil
		}
		xString, ok := x.(string)
		if ok {
			if uuidV4Re.MatchString(xString) {
				return []Conflict{}, nil
			}
		}
		return []Conflict{possibleConflict}, nil
	case "#regex":
		//nolint:gomnd // the "magic" literal constant 2 here is clearer than a synthetic constant symbol
		if len(markerParts) != 2 {
			return []Conflict{possibleConflict}, fmt.Errorf("expected exactly one argument for #regex")
		}
		r, err := regexp.Compile(markerParts[1])
		if err != nil {
			return []Conflict{possibleConflict}, fmt.Errorf("invalid regex argument to #regex: %w", err)
		}
		if xV.Kind() != reflect.String {
			return []Conflict{possibleConflict}, nil
		}
		xString, ok := x.(string)
		if ok {
			if r.MatchString(xString) {
				return []Conflict{}, nil
			}
		}
		return []Conflict{possibleConflict}, nil
		// TODO: "#[num] EXPR"
	}

	return []Conflict{possibleConflict}, fmt.Errorf("unsupported pattern '%s'", marker)
}

func _match(path string, x interface{}, spec interface{}) ([]Conflict, error) {
	possibleConflict := Conflict{
		Path:     path,
		Expected: spec,
		Actual:   x,
	}
	specV := reflect.ValueOf(spec)
	if !specV.IsValid() {
		return _matchZero(path, x), nil
	}

	if specV.Kind() == reflect.String {
		isMarker, specMarker := getMarker(spec)
		if isMarker {
			return _matchWithMarker(path, x, specMarker)
		}
	}

	xV := reflect.ValueOf(x)
	if !xV.IsValid() {
		return []Conflict{possibleConflict}, nil // here we now that spec is non-zero
	}

	if xV.Kind() != specV.Kind() {
		return []Conflict{possibleConflict}, nil
	}

	if m, ok := matchers[specV.Kind()]; ok {
		return m(path, x, spec)
	}
	tX := reflect.TypeOf(x)
	return []Conflict{possibleConflict}, fmt.Errorf("unable to compare %v (type: %v) - kind %v is not supported", x, tX, xV.Kind())
}

func _matchMap(path string, x interface{}, y interface{}) ([]Conflict, error) {
	possibleConflict := Conflict{
		Path:     path,
		Expected: y,
		Actual:   x,
	}
	vX := reflect.ValueOf(x)
	if vX.Kind() != reflect.Map {
		return []Conflict{possibleConflict}, fmt.Errorf("wrong kind for left value, expected Map, got %v", vX.Kind())
	}
	vY := reflect.ValueOf(y)
	if vY.Kind() != reflect.Map {
		return []Conflict{possibleConflict}, fmt.Errorf("wrong kind for pattern value, expected Map, got %v", vX.Kind())
	}

	conflicts, err := _matchMapCheckIteratingObject(path, vX, vY)

	if len(conflicts) > 0 {
		return conflicts, err
	}

	return _matchMapCheckIteratingSpec(path, vX, vY)
}

func _matchMapCheckIteratingObject(path string, vX reflect.Value, vY reflect.Value) ([]Conflict, error) {
	var conflicts []Conflict
	iterX := vX.MapRange()
	for iterX.Next() {
		ySpecValue := vY.MapIndex(iterX.Key())
		if !ySpecValue.IsValid() {
			// missing spec for this key, skip...
			continue
		}
		itemConflicts, err := _match(path+"/"+fmt.Sprint(iterX.Key()), iterX.Value().Interface(), ySpecValue.Interface())
		conflicts = append(conflicts, itemConflicts...)
		if err != nil {
			return conflicts, fmt.Errorf("can't compare map element %s/%v: %w", path, iterX.Key().Interface(), err)
		}
	}
	return conflicts, nil
}

func _matchMapCheckIteratingSpec(path string, vX reflect.Value, vY reflect.Value) ([]Conflict, error) {
	var conflicts []Conflict
	iterY := vY.MapRange()
	for iterY.Next() {
		ySpecValue := iterY.Value()
		xValue := vX.MapIndex(iterY.Key())
		possibleConflict := Conflict{
			Path:     path + "/" + fmt.Sprint(iterY.Key()),
			Expected: iterY.Value().Interface(),
			Actual:   fmt.Sprint(xValue),
		}

		//nolint:wastedassign // defensive programming here...
		var itemConflicts []Conflict
		if ySpecValue.Kind() == reflect.Interface {
			isMarker, marker := getMarker(ySpecValue.Interface())
			if isMarker {
				switch marker {
				case "#notpresent":
					if xValue.IsValid() {
						conflicts = append(conflicts, possibleConflict)
					}
					continue
				case presentMarker:
					if !xValue.IsValid() {
						conflicts = append(conflicts, possibleConflict)
					}
					continue
				}
			}
		}

		if !isMarker(ySpecValue.Interface(), ignoreMarker) {
			if !xValue.IsValid() {
				conflicts = append(conflicts, possibleConflict)
			} else {
				var err error
				itemConflicts, err = _match(path+"/"+fmt.Sprint(iterY.Key()), xValue.Interface(), ySpecValue.Interface())
				conflicts = append(conflicts, itemConflicts...)
				if err != nil {
					return conflicts, fmt.Errorf("can't compare map element %v: %w", iterY.Key().Interface(), err)
				}
			}
		}
	}
	return conflicts, nil
}

func getMarker(y interface{}) (bool, string) {
	specString, ok := y.(string)
	if ok && strings.HasPrefix(specString, "#") {
		return true, specString
	}
	return false, ""
}

func isMarker(y interface{}, marker string) bool {
	isMarker, gotMarker := getMarker(y)
	return isMarker && marker == gotMarker
}

func _matchSlice(path string, x interface{}, y interface{}) ([]Conflict, error) {
	possibleConflict := Conflict{
		Path:     path,
		Expected: y,
		Actual:   x,
	}

	vX := reflect.ValueOf(x)
	if vX.Kind() != reflect.Slice {
		return []Conflict{possibleConflict}, fmt.Errorf("wrong kind for left value, expected Slice, got %v", vX.Kind())
	}
	if reflect.ValueOf(y).Kind() != reflect.Slice {
		return []Conflict{possibleConflict}, fmt.Errorf("wrong kind for pattern value, expected Slice, got %v", vX.Kind())
	}

	vY := reflect.ValueOf(y)
	isArrayOf := false
	var arrayOf interface{}

	//nolint:gomnd // the "magic" literal constant 2 here is clearer than a synthetic constant symbol
	if vY.Len() == 2 {
		first := vY.Index(0).Interface()
		isMarker, firstMarker := getMarker(first)
		if isMarker && firstMarker == "#array-of" {
			isArrayOf = true
			arrayOf = vY.Index(1).Interface()
		}
	} else if vX.Len() != vY.Len() {
		return []Conflict{possibleConflict}, nil
	}

	var conflicts []Conflict
	sliceLen := vX.Len()
	for i := 0; i < sliceLen; i++ {
		var ySpecElem interface{}
		if isArrayOf {
			ySpecElem = arrayOf
		} else {
			ySpecElem = vY.Index(i).Interface()
		}
		itemMatches, err := _match(path+"["+fmt.Sprint(i)+"]", vX.Index(i).Interface(), ySpecElem)
		conflicts = append(conflicts, itemMatches...)
		if err != nil {
			return conflicts, fmt.Errorf("can't compare slice element %v: %w", i, err)
		}
	}
	return conflicts, nil
}

func _matchPrimitive(path string, x interface{}, y interface{}) ([]Conflict, error) {
	if !reflect.DeepEqual(x, y) {
		return []Conflict{
			{
				Path:     path,
				Expected: y,
				Actual:   x,
			},
		}, nil
	}
	return []Conflict{}, nil
}
