package hclparser

import (
	"errors"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/docker/cli/cli/config"
	"github.com/hashicorp/go-cty-funcs/cidr"
	"github.com/hashicorp/go-cty-funcs/crypto"
	"github.com/hashicorp/go-cty-funcs/encoding"
	"github.com/hashicorp/go-cty-funcs/uuid"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

type funcDef struct {
	name           string
	descriptionAlt string
	fn             function.Function
	factory        func() function.Function
}

var stdlibFunctions = []funcDef{
	{name: "absolute", fn: stdlib.AbsoluteFunc},
	{name: "add", fn: stdlib.AddFunc},
	{name: "and", fn: stdlib.AndFunc},
	{name: "base64decode", fn: encoding.Base64DecodeFunc, descriptionAlt: `Decodes a string containing a base64 sequence.`},
	{name: "base64encode", fn: encoding.Base64EncodeFunc, descriptionAlt: `Encodes a string to a base64 sequence.`},
	{name: "basename", factory: basenameFunc},
	{name: "bcrypt", fn: crypto.BcryptFunc, descriptionAlt: `Computes a hash of the given string using the Blowfish cipher.`},
	{name: "byteslen", fn: stdlib.BytesLenFunc},
	{name: "bytesslice", fn: stdlib.BytesSliceFunc},
	{name: "can", fn: tryfunc.CanFunc, descriptionAlt: `Tries to evaluate the expression given in its first argument.`},
	{name: "ceil", fn: stdlib.CeilFunc},
	{name: "chomp", fn: stdlib.ChompFunc},
	{name: "chunklist", fn: stdlib.ChunklistFunc},
	{name: "cidrhost", fn: cidr.HostFunc, descriptionAlt: `Calculates a full host IP address within a given IP network address prefix.`},
	{name: "cidrnetmask", fn: cidr.NetmaskFunc, descriptionAlt: `Converts an IPv4 address prefix given in CIDR notation into a subnet mask address.`},
	{name: "cidrsubnet", fn: cidr.SubnetFunc, descriptionAlt: `Calculates a subnet address within a given IP network address prefix.`},
	{name: "cidrsubnets", fn: cidr.SubnetsFunc, descriptionAlt: `Calculates many consecutive subnet addresses at once, rather than just a single subnet extension.`},
	{name: "coalesce", fn: stdlib.CoalesceFunc},
	{name: "coalescelist", fn: stdlib.CoalesceListFunc},
	{name: "compact", fn: stdlib.CompactFunc},
	{name: "concat", fn: stdlib.ConcatFunc},
	{name: "contains", fn: stdlib.ContainsFunc},
	{name: "convert", fn: typeexpr.ConvertFunc, descriptionAlt: `Converts a value to a specified type constraint, using HCL's customdecode extension for type expression support.`},
	{name: "csvdecode", fn: stdlib.CSVDecodeFunc},
	{name: "dirname", factory: dirnameFunc},
	{name: "distinct", fn: stdlib.DistinctFunc},
	{name: "divide", fn: stdlib.DivideFunc},
	{name: "element", fn: stdlib.ElementFunc},
	{name: "equal", fn: stdlib.EqualFunc},
	{name: "flatten", fn: stdlib.FlattenFunc},
	{name: "floor", fn: stdlib.FloorFunc},
	{name: "format", fn: stdlib.FormatFunc},
	{name: "formatdate", fn: stdlib.FormatDateFunc},
	{name: "formatlist", fn: stdlib.FormatListFunc},
	{name: "greaterthan", fn: stdlib.GreaterThanFunc},
	{name: "greaterthanorequalto", fn: stdlib.GreaterThanOrEqualToFunc},
	{name: "hasindex", fn: stdlib.HasIndexFunc},
	{name: "homedir", factory: homedirFunc},
	{name: "indent", fn: stdlib.IndentFunc},
	{name: "index", fn: stdlib.IndexFunc},
	{name: "indexof", factory: indexOfFunc},
	{name: "int", fn: stdlib.IntFunc},
	{name: "join", fn: stdlib.JoinFunc},
	{name: "jsondecode", fn: stdlib.JSONDecodeFunc},
	{name: "jsonencode", fn: stdlib.JSONEncodeFunc},
	{name: "keys", fn: stdlib.KeysFunc},
	{name: "length", fn: stdlib.LengthFunc},
	{name: "lessthan", fn: stdlib.LessThanFunc},
	{name: "lessthanorequalto", fn: stdlib.LessThanOrEqualToFunc},
	{name: "log", fn: stdlib.LogFunc},
	{name: "lookup", fn: stdlib.LookupFunc},
	{name: "lower", fn: stdlib.LowerFunc},
	{name: "max", fn: stdlib.MaxFunc},
	{name: "md5", fn: crypto.Md5Func, descriptionAlt: `Computes the MD5 hash of a given string and encodes it with hexadecimal digits.`},
	{name: "merge", fn: stdlib.MergeFunc},
	{name: "min", fn: stdlib.MinFunc},
	{name: "modulo", fn: stdlib.ModuloFunc},
	{name: "multiply", fn: stdlib.MultiplyFunc},
	{name: "negate", fn: stdlib.NegateFunc},
	{name: "not", fn: stdlib.NotFunc},
	{name: "notequal", fn: stdlib.NotEqualFunc},
	{name: "or", fn: stdlib.OrFunc},
	{name: "parseint", fn: stdlib.ParseIntFunc},
	{name: "pow", fn: stdlib.PowFunc},
	{name: "range", fn: stdlib.RangeFunc},
	{name: "regex_replace", fn: stdlib.RegexReplaceFunc},
	{name: "regex", fn: stdlib.RegexFunc},
	{name: "regexall", fn: stdlib.RegexAllFunc},
	{name: "replace", fn: stdlib.ReplaceFunc},
	{name: "reverse", fn: stdlib.ReverseFunc},
	{name: "reverselist", fn: stdlib.ReverseListFunc},
	{name: "rsadecrypt", fn: crypto.RsaDecryptFunc, descriptionAlt: `Decrypts an RSA-encrypted ciphertext.`},
	{name: "sanitize", factory: sanitizeFunc},
	{name: "sethaselement", fn: stdlib.SetHasElementFunc},
	{name: "setintersection", fn: stdlib.SetIntersectionFunc},
	{name: "setproduct", fn: stdlib.SetProductFunc},
	{name: "setsubtract", fn: stdlib.SetSubtractFunc},
	{name: "setsymmetricdifference", fn: stdlib.SetSymmetricDifferenceFunc},
	{name: "setunion", fn: stdlib.SetUnionFunc},
	{name: "sha1", fn: crypto.Sha1Func, descriptionAlt: `Computes the SHA1 hash of a given string and encodes it with hexadecimal digits.`},
	{name: "sha256", fn: crypto.Sha256Func, descriptionAlt: `Computes the SHA256 hash of a given string and encodes it with hexadecimal digits.`},
	{name: "sha512", fn: crypto.Sha512Func, descriptionAlt: `Computes the SHA512 hash of a given string and encodes it with hexadecimal digits.`},
	{name: "signum", fn: stdlib.SignumFunc},
	{name: "slice", fn: stdlib.SliceFunc},
	{name: "sort", fn: stdlib.SortFunc},
	{name: "split", fn: stdlib.SplitFunc},
	{name: "strlen", fn: stdlib.StrlenFunc},
	{name: "substr", fn: stdlib.SubstrFunc},
	{name: "subtract", fn: stdlib.SubtractFunc},
	{name: "timeadd", fn: stdlib.TimeAddFunc},
	{name: "timestamp", factory: timestampFunc},
	{name: "title", fn: stdlib.TitleFunc},
	{name: "trim", fn: stdlib.TrimFunc},
	{name: "trimprefix", fn: stdlib.TrimPrefixFunc},
	{name: "trimspace", fn: stdlib.TrimSpaceFunc},
	{name: "trimsuffix", fn: stdlib.TrimSuffixFunc},
	{name: "try", fn: tryfunc.TryFunc, descriptionAlt: `Variadic function that tries to evaluate all of is arguments in sequence until one succeeds, in which case it returns that result, or returns an error if none of them succeed.`},
	{name: "unixtimestampparse", factory: unixtimestampParseFunc},
	{name: "upper", fn: stdlib.UpperFunc},
	{name: "urlencode", fn: encoding.URLEncodeFunc, descriptionAlt: `Applies URL encoding to a given string.`},
	{name: "uuidv4", fn: uuid.V4Func, descriptionAlt: `Generates and returns a Type-4 UUID in the standard hexadecimal string format.`},
	{name: "uuidv5", fn: uuid.V5Func, descriptionAlt: `Generates and returns a Type-5 UUID in the standard hexadecimal string format.`},
	{name: "values", fn: stdlib.ValuesFunc},
	{name: "zipmap", fn: stdlib.ZipmapFunc},
}

// indexOfFunc constructs a function that finds the element index for a given
// value in a list.
func indexOfFunc() function.Function {
	return function.New(&function.Spec{
		Description: `Finds the element index for a given value in a list.`,
		Params: []function.Parameter{
			{
				Name: "list",
				Type: cty.DynamicPseudoType,
			},
			{
				Name: "value",
				Type: cty.DynamicPseudoType,
			},
		},
		Type: function.StaticReturnType(cty.Number),
		Impl: func(args []cty.Value, retType cty.Type) (ret cty.Value, err error) {
			if !args[0].Type().IsListType() && !args[0].Type().IsTupleType() {
				return cty.NilVal, errors.New("argument must be a list or tuple")
			}

			if !args[0].IsKnown() {
				return cty.UnknownVal(cty.Number), nil
			}

			if args[0].LengthInt() == 0 { // Easy path
				return cty.NilVal, errors.New("cannot search an empty list")
			}

			for it := args[0].ElementIterator(); it.Next(); {
				i, v := it.Element()
				eq, err := stdlib.Equal(v, args[1])
				if err != nil {
					return cty.NilVal, err
				}
				if !eq.IsKnown() {
					return cty.UnknownVal(cty.Number), nil
				}
				if eq.True() {
					return i, nil
				}
			}
			return cty.NilVal, errors.New("item not found")
		},
	})
}

// basenameFunc constructs a function that returns the last element of a path.
func basenameFunc() function.Function {
	return function.New(&function.Spec{
		Description: `Returns the last element of a path.`,
		Params: []function.Parameter{
			{
				Name: "path",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			in := args[0].AsString()
			return cty.StringVal(path.Base(in)), nil
		},
	})
}

// dirnameFunc constructs a function that returns the directory of a path.
func dirnameFunc() function.Function {
	return function.New(&function.Spec{
		Description: `Returns the directory of a path.`,
		Params: []function.Parameter{
			{
				Name: "path",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			in := args[0].AsString()
			return cty.StringVal(path.Dir(in)), nil
		},
	})
}

// sanitizyFunc constructs a function that replaces all non-alphanumeric characters with a underscore,
// leaving only characters that are valid for a Bake target name.
func sanitizeFunc() function.Function {
	return function.New(&function.Spec{
		Description: `Replaces all non-alphanumeric characters with a underscore, leaving only characters that are valid for a Bake target name.`,
		Params: []function.Parameter{
			{
				Name: "name",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			in := args[0].AsString()
			// only [a-zA-Z0-9_-]+ is allowed
			var b strings.Builder
			for _, r := range in {
				if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
					b.WriteRune(r)
				} else {
					b.WriteRune('_')
				}
			}
			return cty.StringVal(b.String()), nil
		},
	})
}

// timestampFunc constructs a function that returns a string representation of the current date and time.
//
// This function was imported from Terraform's datetime utilities.
func timestampFunc() function.Function {
	return function.New(&function.Spec{
		Description: `Returns a string representation of the current date and time.`,
		Params:      []function.Parameter{},
		Type:        function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return cty.StringVal(time.Now().UTC().Format(time.RFC3339)), nil
		},
	})
}

// homedirFunc constructs a function that returns the current user's home directory.
func homedirFunc() function.Function {
	return function.New(&function.Spec{
		Description: `Returns the current user's home directory.`,
		Params:      []function.Parameter{},
		Type:        function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			home, err := os.UserHomeDir()
			if err != nil {
				if home == "" && runtime.GOOS != "windows" {
					if u, err := user.Current(); err == nil {
						return cty.StringVal(u.HomeDir), nil
					}
				}
				return cty.StringVal(filepath.Dir(config.Dir())), nil
			}
			return cty.StringVal(home), nil
		},
	})
}

// unixtimestampParseFunc, given a unix timestamp integer, will parse and
// return an object representation of that date and time
//
// This function is similar to the `unix_timestamp_parse` function in Terraform:
// https://registry.terraform.io/providers/hashicorp/time/latest/docs/functions/unix_timestamp_parse
func unixtimestampParseFunc() function.Function {
	return function.New(&function.Spec{
		Description: `Given a unix timestamp integer, will parse and return an object representation of that date and time. A unix timestamp is the number of seconds elapsed since January 1, 1970 UTC.`,
		Params: []function.Parameter{
			{
				Name:        "unix_timestamp",
				Description: "Unix Timestamp integer to parse",
				Type:        cty.Number,
			},
		},
		Type: function.StaticReturnType(cty.Object(map[string]cty.Type{
			"year":         cty.Number,
			"year_day":     cty.Number,
			"day":          cty.Number,
			"month":        cty.Number,
			"month_name":   cty.String,
			"weekday":      cty.Number,
			"weekday_name": cty.String,
			"hour":         cty.Number,
			"minute":       cty.Number,
			"second":       cty.Number,
			"rfc3339":      cty.String,
			"iso_year":     cty.Number,
			"iso_week":     cty.Number,
		})),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			ts, _ := args[0].AsBigFloat().Int64()
			unixTime := time.Unix(ts, 0).UTC()
			isoYear, isoWeek := unixTime.ISOWeek()
			return cty.ObjectVal(map[string]cty.Value{
				"year":         cty.NumberIntVal(int64(unixTime.Year())),
				"year_day":     cty.NumberIntVal(int64(unixTime.YearDay())),
				"day":          cty.NumberIntVal(int64(unixTime.Day())),
				"month":        cty.NumberIntVal(int64(unixTime.Month())),
				"month_name":   cty.StringVal(unixTime.Month().String()),
				"weekday":      cty.NumberIntVal(int64(unixTime.Weekday())),
				"weekday_name": cty.StringVal(unixTime.Weekday().String()),
				"hour":         cty.NumberIntVal(int64(unixTime.Hour())),
				"minute":       cty.NumberIntVal(int64(unixTime.Minute())),
				"second":       cty.NumberIntVal(int64(unixTime.Second())),
				"rfc3339":      cty.StringVal(unixTime.Format(time.RFC3339)),
				"iso_year":     cty.NumberIntVal(int64(isoYear)),
				"iso_week":     cty.NumberIntVal(int64(isoWeek)),
			}), nil
		},
	})
}

func Stdlib() map[string]function.Function {
	funcs := make(map[string]function.Function, len(stdlibFunctions))
	for _, v := range stdlibFunctions {
		if v.factory != nil {
			funcs[v.name] = v.factory()
		} else {
			funcs[v.name] = v.fn
		}
	}
	return funcs
}

func StdlibFuncDescription(name string) string {
	for _, v := range stdlibFunctions {
		if v.name != name {
			continue
		}
		if v.descriptionAlt != "" {
			return v.descriptionAlt
		}
		if v.factory != nil {
			return v.factory().Description()
		}
		return v.fn.Description()
	}
	return ""
}
