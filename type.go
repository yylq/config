// Copyright 2009  The "config" Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// Substitutes values, calculated by callback, on matching regex
func (c *Config) computeVar(beforeValue *string, regx *regexp.Regexp, headsz, tailsz int, withVar func(*string) string) (*string, error) {
	var i int
	computedVal := beforeValue
	for i = 0; i < _DEPTH_VALUES; i++ { // keep a sane depth

		vr := regx.FindStringSubmatchIndex(*computedVal)
		if len(vr) == 0 {
			break
		}

		varname := (*computedVal)[vr[headsz]:vr[headsz+1]]
		varVal := withVar(&varname)
		if varVal == "" {
			return &varVal, errors.New(fmt.Sprintf("Option not found: %s", varname))
		}

		// substitute by new value and take off leading '%(' and trailing ')s'
		//  %(foo)s => headsz=2, tailsz=2
		//  ${foo}  => headsz=2, tailsz=1
		newVal := (*computedVal)[0:vr[headsz]-headsz] + varVal + (*computedVal)[vr[headsz+1]+tailsz:]
		computedVal = &newVal
	}

	if i == _DEPTH_VALUES {
		retVal := ""
		return &retVal,
			fmt.Errorf("Possible cycle while unfolding variables: max depth of %d reached", _DEPTH_VALUES)
	}

	return computedVal, nil
}

// Bool has the same behaviour as String but converts the response to bool.
// See "boolString" for string values converted to bool.
func (c *Config) Bool(section string, option string) (value bool, err error) {
	sv, err := c.String(section, option)
	if err != nil {
		return false, err
	}

	value, ok := boolString[strings.ToLower(sv)]
	if !ok {
		return false, errors.New("could not parse bool value: " + sv)
	}

	return value, nil
}

// Float has the same behaviour as String but converts the response to float.
func (c *Config) Float(section string, option string) (value float64, err error) {
	sv, err := c.String(section, option)
	if err == nil {
		value, err = strconv.ParseFloat(sv, 64)
	}

	return value, err
}

// Int has the same behaviour as String but converts the response to int.
func (c *Config) Int(section string, option string) (value int, err error) {
	sv, err := c.String(section, option)
	if err == nil {
		value, err = strconv.Atoi(sv)
	}

	return value, err
}

// RawString gets the (raw) string value for the given option in the section.
// The raw string value is not subjected to unfolding, which was illustrated in
// the beginning of this documentation.
//
// It returns an error if either the section or the option do not exist.
func (c *Config) RawString(section string, option string) (value string, err error) {
	if _, ok := c.data[section]; ok {
		if tValue, ok := c.data[section][option]; ok {
			return tValue.v, nil
		}
	}
	return c.RawStringDefault(option)
}

// RawStringDefault gets the (raw) string value for the given option from the
// DEFAULT section.
//
// It returns an error if the option does not exist in the DEFAULT section.
func (c *Config) RawStringDefault(option string) (value string, err error) {
	if tValue, ok := c.data[DEFAULT_SECTION][option]; ok {
		return tValue.v, nil
	}
	return "", OptionError(option)
}

// String gets the string value for the given option in the section.
// If the value needs to be unfolded (see e.g. %(host)s example in the beginning
// of this documentation), then String does this unfolding automatically, up to
// _DEPTH_VALUES number of iterations.
//
// It returns an error if either the section or the option do not exist, or the
// unfolding cycled.
func (c *Config) String(section string, option string) (value string, err error) {
	value, err = c.RawString(section, option)
	if err != nil {
		return "", err
	}

	// % variables
	computedVal, err := c.computeVar(&value, varRegExp, 2, 2, func(varName *string) string {
		lowerVar := *varName
		// search variable in default section as well as current section
		varVal, _ := c.data[DEFAULT_SECTION][lowerVar]
		if _, ok := c.data[section][lowerVar]; ok {
			varVal = c.data[section][lowerVar]
		}
		return varVal.v
	})
	value = *computedVal

	if err != nil {
		return value, err
	}

	// $ environment variables
	computedVal, err = c.computeVar(&value, envVarRegExp, 2, 1, func(varName *string) string {
		return os.Getenv(*varName)
	})
	value = *computedVal
	return value, err
}

var ErrNotFound = errors.New("not found")
var ErrUnsupportedType = errors.New("unsupported type")

func (c *Config) ParseConf(st interface{}) error {
	v := reflect.ValueOf(st)
	k := v.Kind()

	if k != reflect.Ptr && k != reflect.Interface {
		return ErrUnsupportedType
	} else if v.IsNil() {
		return ErrUnsupportedType
	}
	fmt.Printf("%v\n", k)
	e := v.Elem()

	switch e.Kind() {
	case reflect.Struct:
		return c.loadStruct(e)

	case reflect.Interface, reflect.Ptr:
		return c.ParseConf(e)
	default:
		return ErrUnsupportedType
	}
}
func (c *Config) loadStruct(v reflect.Value) error {
	fmt.Printf("loadStruct\n")
	t := v.Type()
	n := t.NumField()
	for i := 0; i < n; i++ {
		sec, opt := fieldName(t.Field(i))

		if sec == "" || opt == "" {
			continue
		}
		f := v.Field(i)
		err := c.loadSecOpt(f, sec, opt)
		if err != nil && err != ErrNotFound {
			return err
		}
	}
	return nil
}

func (c *Config) loadSecOpt(f reflect.Value, sec string, opt string) error {
	fmt.Printf("loadSecOpt %s-%s\n", sec, opt)
	switch f.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return c.loadFieldInt(f, sec, opt)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return c.loadFieldUint(f, sec, opt)
	//case reflect.Float32, reflect.Float64:
	//	return c.loadFieldFloat(f, name)
	case reflect.String:
		return c.loadFieldString(f, sec, opt)
	case reflect.Bool:
		return c.loadFieldBool(f, sec, opt)
	case reflect.Slice:
		return c.loadFieldSlice(f, sec, opt)
	default:
		return errors.New(fmt.Sprintf("unsupported type:[%s-%s]: %s", sec, opt, f.Kind()))
	}
}
func (c *Config) loadFieldInt(f reflect.Value, sec string, opt string) error {

	i, err := c.Int(sec, opt)
	if err != nil {
		return err
	}
	f.SetInt(int64(i))
	return nil
}

func (c *Config) loadFieldUint(f reflect.Value, sec string, opt string) error {

	i, err := c.Int(sec, opt)
	if err != nil {
		return err
	}
	f.SetUint(uint64(i))
	return nil
}

func (c *Config) loadFieldBool(f reflect.Value, sec string, opt string) error {

	i, err := c.Bool(sec, opt)
	if err != nil {
		return err
	}
	f.SetBool(i)
	return nil
}
func (c *Config) transvalue(kind reflect.Kind, v string) (reflect.Value, error) {
	switch kind {
	case reflect.String:
		return reflect.ValueOf(v), nil

	case reflect.Bool:
		i, ok := boolString[strings.ToLower(v)]
		if !ok {
			return reflect.ValueOf(false), errors.New("could not parse bool value: " + v)
		}
		return reflect.ValueOf(i), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.Atoi(v)
		return reflect.ValueOf(i), err
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		i, err := strconv.Atoi(v)
		return reflect.ValueOf(uint64(i)), err
	case reflect.Float32:
		f, err := strconv.ParseFloat(v, 32)
		return reflect.ValueOf(float32(f)), err
	case reflect.Float64:
		f, err := strconv.ParseFloat(v, 64)
		return reflect.ValueOf(float64(f)), err
	}
	return reflect.ValueOf(nil), ErrUnsupportedType
}
func (c *Config) loadFieldSlice(f reflect.Value, sec string, opt string) error {
	fmt.Printf("loadFieldSlice %s-%s\n", sec, opt)
	v, err := c.String(sec, opt)
	if err != nil {
		return err
	}
	fmt.Printf("loadFieldSlice  %v ,%s-%s value:%s\n", f.Type(), sec, opt, v)
	e := f.Type().Elem()
	ss := strings.Split(v, ",")
	newv := reflect.MakeSlice(f.Type(), len(ss), len(ss))
	for i := 0; i < len(ss); i++ {
		v, err := c.transvalue(e.Kind(), ss[i])
		if err != nil {
			return err
		}
		newv.Index(i).Set(v)
	}
	f.Set(newv)
	return nil
}

func (c *Config) loadFieldString(f reflect.Value, sec string, opt string) error {

	i, err := c.String(sec, opt)
	if err != nil {
		return err
	}
	f.SetString(i)
	return nil
}
func fieldName(f reflect.StructField) (string, string) {
	if f.Anonymous {
		return "", ""
	}
	tag := f.Tag.Get("config")
	if tag != "" {
		if tag == "-" {
			return "", ""
		}
		tagParts := strings.Split(tag, "-")
		if len(tagParts) >= 1 {
			return strings.TrimSpace(tagParts[0]), strings.TrimSpace(tagParts[1])
		}
	}
	return "", ""
}
