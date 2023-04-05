package hclparser

import (
	"time"

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

var stdlibFunctions = map[string]function.Function{
	"absolute":               stdlib.AbsoluteFunc,
	"add":                    stdlib.AddFunc,
	"and":                    stdlib.AndFunc,
	"base64decode":           encoding.Base64DecodeFunc,
	"base64encode":           encoding.Base64EncodeFunc,
	"bcrypt":                 crypto.BcryptFunc,
	"byteslen":               stdlib.BytesLenFunc,
	"bytesslice":             stdlib.BytesSliceFunc,
	"can":                    tryfunc.CanFunc,
	"ceil":                   stdlib.CeilFunc,
	"chomp":                  stdlib.ChompFunc,
	"chunklist":              stdlib.ChunklistFunc,
	"cidrhost":               cidr.HostFunc,
	"cidrnetmask":            cidr.NetmaskFunc,
	"cidrsubnet":             cidr.SubnetFunc,
	"cidrsubnets":            cidr.SubnetsFunc,
	"coalesce":               stdlib.CoalesceFunc,
	"coalescelist":           stdlib.CoalesceListFunc,
	"compact":                stdlib.CompactFunc,
	"concat":                 stdlib.ConcatFunc,
	"contains":               stdlib.ContainsFunc,
	"convert":                typeexpr.ConvertFunc,
	"csvdecode":              stdlib.CSVDecodeFunc,
	"distinct":               stdlib.DistinctFunc,
	"divide":                 stdlib.DivideFunc,
	"element":                stdlib.ElementFunc,
	"equal":                  stdlib.EqualFunc,
	"flatten":                stdlib.FlattenFunc,
	"floor":                  stdlib.FloorFunc,
	"format":                 stdlib.FormatFunc,
	"formatdate":             stdlib.FormatDateFunc,
	"formatlist":             stdlib.FormatListFunc,
	"greaterthan":            stdlib.GreaterThanFunc,
	"greaterthanorequalto":   stdlib.GreaterThanOrEqualToFunc,
	"hasindex":               stdlib.HasIndexFunc,
	"indent":                 stdlib.IndentFunc,
	"index":                  stdlib.IndexFunc,
	"int":                    stdlib.IntFunc,
	"join":                   stdlib.JoinFunc,
	"jsondecode":             stdlib.JSONDecodeFunc,
	"jsonencode":             stdlib.JSONEncodeFunc,
	"keys":                   stdlib.KeysFunc,
	"length":                 stdlib.LengthFunc,
	"lessthan":               stdlib.LessThanFunc,
	"lessthanorequalto":      stdlib.LessThanOrEqualToFunc,
	"log":                    stdlib.LogFunc,
	"lookup":                 stdlib.LookupFunc,
	"lower":                  stdlib.LowerFunc,
	"max":                    stdlib.MaxFunc,
	"md5":                    crypto.Md5Func,
	"merge":                  stdlib.MergeFunc,
	"min":                    stdlib.MinFunc,
	"modulo":                 stdlib.ModuloFunc,
	"multiply":               stdlib.MultiplyFunc,
	"negate":                 stdlib.NegateFunc,
	"not":                    stdlib.NotFunc,
	"notequal":               stdlib.NotEqualFunc,
	"or":                     stdlib.OrFunc,
	"parseint":               stdlib.ParseIntFunc,
	"pow":                    stdlib.PowFunc,
	"range":                  stdlib.RangeFunc,
	"regex_replace":          stdlib.RegexReplaceFunc,
	"regex":                  stdlib.RegexFunc,
	"regexall":               stdlib.RegexAllFunc,
	"replace":                stdlib.ReplaceFunc,
	"reverse":                stdlib.ReverseFunc,
	"reverselist":            stdlib.ReverseListFunc,
	"rsadecrypt":             crypto.RsaDecryptFunc,
	"sethaselement":          stdlib.SetHasElementFunc,
	"setintersection":        stdlib.SetIntersectionFunc,
	"setproduct":             stdlib.SetProductFunc,
	"setsubtract":            stdlib.SetSubtractFunc,
	"setsymmetricdifference": stdlib.SetSymmetricDifferenceFunc,
	"setunion":               stdlib.SetUnionFunc,
	"sha1":                   crypto.Sha1Func,
	"sha256":                 crypto.Sha256Func,
	"sha512":                 crypto.Sha512Func,
	"signum":                 stdlib.SignumFunc,
	"slice":                  stdlib.SliceFunc,
	"sort":                   stdlib.SortFunc,
	"split":                  stdlib.SplitFunc,
	"strlen":                 stdlib.StrlenFunc,
	"substr":                 stdlib.SubstrFunc,
	"subtract":               stdlib.SubtractFunc,
	"timeadd":                stdlib.TimeAddFunc,
	"timestamp":              timestampFunc,
	"title":                  stdlib.TitleFunc,
	"trim":                   stdlib.TrimFunc,
	"trimprefix":             stdlib.TrimPrefixFunc,
	"trimspace":              stdlib.TrimSpaceFunc,
	"trimsuffix":             stdlib.TrimSuffixFunc,
	"try":                    tryfunc.TryFunc,
	"upper":                  stdlib.UpperFunc,
	"urlencode":              encoding.URLEncodeFunc,
	"uuidv4":                 uuid.V4Func,
	"uuidv5":                 uuid.V5Func,
	"values":                 stdlib.ValuesFunc,
	"zipmap":                 stdlib.ZipmapFunc,
}

// timestampFunc constructs a function that returns a string representation of the current date and time.
//
// This function was imported from terraform's datetime utilities.
var timestampFunc = function.New(&function.Spec{
	Params: []function.Parameter{},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		return cty.StringVal(time.Now().UTC().Format(time.RFC3339)), nil
	},
})

func Stdlib() map[string]function.Function {
	funcs := make(map[string]function.Function, len(stdlibFunctions))
	for k, v := range stdlibFunctions {
		funcs[k] = v
	}
	return funcs
}
