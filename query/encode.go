// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package query implements encoding of structs into URL query parameters.
//
// As a simple example:
//
// 	type Options struct {
// 		Query   string `url:"q"`
// 		ShowAll bool   `url:"all"`
// 		Page    int    `url:"page"`
// 	}
//
// 	opt := Options{ "foo", true, 2 }
// 	v, _ := query.Values(opt)
// 	fmt.Print(v.Encode()) // will output: "q=foo&all=true&page=2"
//
// The exact mapping between Go values and url.Values is described in the
// documentation for the Values() function.
package query

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var timeType = reflect.TypeOf(time.Time{})

// Values returns the url.Values encoding of v.
//
// Values expects to be passed a struct, and traverses it recursively using the
// following encoding rules.
//
// Each exported struct field is encoded as a URL parameter unless
//
//	- the field's tag is "-", or
//	- the field is empty and its tag specifies the "omitempty" option
//
// The empty values are false, 0, any nil pointer or interface value, any array
// slice, map, or string of length zero, and any time.Time that returns true
// for IsZero().
//
// The URL parameter name defaults to the struct field name but can be
// specified in the struct field's tag value.  The "url" key in the struct
// field's tag value is the key name, followed by an optional comma and
// options.  For example:
//
// 	// Field is ignored by this package.
// 	Field int `url:"-"`
//
// 	// Field appears as URL parameter "myName".
// 	Field int `url:"myName"`
//
// 	// Field appears as URL parameter "myName" and the field is omitted if
// 	// its value is empty
// 	Field int `url:"myName,omitempty"`
//
// 	// Field appears as URL parameter "Field" (the default), but the field
// 	// is skipped if empty.  Note the leading comma.
// 	Field int `url:",omitempty"`
//
// For encoding individual field values, the following type-dependent rules
// apply:
//
// Boolean values default to encoding as the strings "true" or "false".
// Including the "int" option signals that the field should be encoded as the
// strings "1" or "0".
//
// time.Time values default to encoding as RFC3339 timestamps.  Including the
// "unix" option signals that the field should be encoded as a Unix time (see
// time.Unix())
//
// Slice and Array values default to encoding as multiple URL values of the
// same name.  Including the "comma" option signals that the field should be
// encoded as a single comma-delimited value.  Including the "space" option
// similarly encodes the value as a single space-delimited string.
//
// Anonymous struct fields are usually encoded as if their inner exported
// fields were fields in the outer struct, subject to the standard Go
// visibility rules.  An anonymous struct field with a name given in its URL
// tag is treated as having that name, rather than being anonymous.
//
// Non-nil pointer values are encoded as the value pointed to.
//
// All other values are encoded using their default string representation.
//
// Multiple fields that encode to the same URL parameter name will be included
// as multiple URL values of the same name.
func Values(v interface{}) (url.Values, error) {
	values := &url.Values{}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, errors.New("query: Values() expects non-nil value")
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("query: Values() expects struct input. Got %v", val.Kind())
	}

	reflectValue(values, val)

	return *values, nil
}

func reflectValue(values *url.Values, val reflect.Value) {
	embedded := []reflect.Value{}

	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		sf := typ.Field(i)
		if sf.PkgPath != "" { // unexported
			continue
		}

		sv := val.Field(i)
		tag := sf.Tag.Get("url")
		if tag == "-" {
			continue
		}
		name, opts := parseTag(tag)
		if name == "" {
			if sf.Anonymous && sv.Kind() == reflect.Struct {
				// save embedded struct for later processing
				embedded = append(embedded, sv)
				continue
			}

			name = sf.Name
		}

		if opts.Contains("omitempty") && isEmptyValue(sv) {
			continue
		}

		switch sv.Kind() {
		case reflect.Slice, reflect.Array:
			var del byte
			if opts.Contains("comma") {
				del = ','
			} else if opts.Contains("space") {
				del = ' '
			}

			if del != 0 {
				s := new(bytes.Buffer)
				first := true
				for i := 0; i < sv.Len(); i++ {
					if first {
						first = false
					} else {
						s.WriteByte(del)
					}
					s.WriteString(valueString(sv.Index(i), opts))
				}
				values.Add(name, s.String())
			} else {
				for i := 0; i < sv.Len(); i++ {
					values.Add(name, valueString(sv.Index(i), opts))
				}
			}
		default:
			values.Add(name, valueString(sv, opts))
		}
	}

	for _, f := range embedded {
		reflectValue(values, f)
	}
}

// valueString returns the string representation of a value.
func valueString(v reflect.Value, opts tagOptions) string {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Bool:
		if opts.Contains("int") {
			if v.Bool() {
				return "1"
			}
			return "0"
		}
	}

	switch v.Type() {
	case timeType:
		t := v.Interface().(time.Time)
		if opts.Contains("unix") {
			return strconv.FormatInt(t.Unix(), 10)
		}
		return t.Format(time.RFC3339)
	}

	return fmt.Sprint(v.Interface())
}

// isEmptyValue checks if a value should be considered empty for the purposes
// of omitting fields with the "omitempty" option.
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}

	switch v.Type() {
	case timeType:
		return v.Interface().(time.Time).IsZero()
	}

	return false
}

// tagOptions is the string following a comma in a struct field's "url" tag, or
// the empty string. It does not include the leading comma.
type tagOptions []string

// parseTag splits a struct field's url tag into its name and comma-separated
// options.
func parseTag(tag string) (string, tagOptions) {
	s := strings.Split(tag, ",")
	return s[0], s[1:]
}

// Contains checks whether the tagOptions contains the specified option.
func (o tagOptions) Contains(option string) bool {
	for _, s := range o {
		if s == option {
			return true
		}
	}
	return false
}
