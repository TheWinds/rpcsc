package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"sort"
	"strings"
	"text/template"

	macaron "gopkg.in/macaron.v1"
)

func main() {
	m := macaron.Classic()
	m.Use(macaron.Renderer())
	m.Get("/", func(ctx *macaron.Context) {
		ctx.HTML(200, "index.html")
	})
	m.Post("/gen", func(ctx *macaron.Context) string {
		req := new(reqModel)
		err := bindJSON(ctx, req)
		if err != nil {
			return err.Error()
		}
		output, err := genParseFunc(req.RPCStruct, req.ModelStruct, req.RPCPackageName)
		if err != nil {
			return err.Error()
		}
		return output
	})
	m.Run()

}

type reqModel struct {
	ModelStruct    string `json:"model_struct"`
	RPCStruct      string `json:"rpc_struct"`
	RPCPackageName string `json:"rpc_package_name"`
}

func bindJSON(ctx *macaron.Context, v interface{}) error {
	return json.NewDecoder(ctx.Req.Body().ReadCloser()).Decode(v)
}

type structField struct {
	Name    string
	Type    string
	Comment string
}

type structInfo struct {
	Name     string
	Fields   []structField
	FieldNum int
}

type fieldValue struct {
	FieldName string
	Value     string
}

// 渲染
type renderModel struct {
	RPCPackageName  string
	RPCStructName   string
	ModelStructName string
	FieldValues     []fieldValue
}

// 生成genParseFunc
func genParseFunc(rpcStructSrc, modelStructSrc, rpcStructPackageName string) (string, error) {
	if rpcStructPackageName != "" {
		rpcStructPackageName += "."
	}
	rpcStructInfo, err := getStructInfo(rpcStructSrc)
	if err != nil {
		return "", err
	}
	modelStructInfo, err := getStructInfo(modelStructSrc)
	if err != nil {
		return "", err
	}
	// 字段映射
	// model struct field => rpc struct field
	fieldMap := map[structField]structField{}
	for _, rpcField := range rpcStructInfo.Fields {
		mapFrom := findStructField(rpcField.Name, modelStructInfo.Fields)
		if mapFrom == nil {
			fmt.Printf("filed %+v cant map\n", rpcField)
			continue
		}
		fieldMap[*mapFrom] = rpcField
	}

	funcRenderModel := &renderModel{
		RPCPackageName:  rpcStructPackageName,
		ModelStructName: modelStructInfo.Name,
		RPCStructName:   rpcStructInfo.Name,
		FieldValues:     make([]fieldValue, 0, len(fieldMap)),
	}

	for key, value := range fieldMap {
		fv := fieldValue{
			FieldName: value.Name,
			Value:     "model." + key.Name,
		}
		if key.Type != value.Type {
			fv.Value = fmt.Sprintf("%s(%s)", value.Type, fv.Value)
		}
		funcRenderModel.FieldValues = append(funcRenderModel.FieldValues, fv)
	}
	// 排序
	sort.Slice(funcRenderModel.FieldValues, func(i, j int) bool {
		return funcRenderModel.FieldValues[i].FieldName < funcRenderModel.FieldValues[j].FieldName
	})
	// 渲染结果
	tmpl := template.New("tmpl")
	if err != nil {
		return "", err
	}
	tmplFile, _ := ioutil.ReadFile("template")
	tmpl, err = tmpl.Parse(string(tmplFile))
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, funcRenderModel)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func findStructField(name string, fields []structField) *structField {
	for _, field := range fields {
		if field.Name == name || getRPCFieldNameFromComment(field.Comment) == name {
			return &field
		}
	}
	return nil
}

// 从备注中获取rpc中的字段名
func getRPCFieldNameFromComment(comment string) string {
	if comment == "" {
		return ""
	}
	//rpc:xxx
	comment = strings.TrimSpace(comment)
	comment = strings.Replace(comment, "//rpc:", "", -1)
	if strings.Contains(comment, " ") {
		return strings.Split(comment, " ")[0]
	}
	return comment
}

// 获取结构体信息
func getStructInfo(structDef string) (*structInfo, error) {
	src := "package main \n" + structDef
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	info := new(structInfo)
	info.Fields = make([]structField, 0)
	// 从语法树获取内容 这个比较乱
	ast.Inspect(f, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.TypeSpec:
			structNode := node.(*ast.TypeSpec)
			info.Name = structNode.Name.Name
			structFields := (structNode.Type).(*ast.StructType).Fields
			for _, field := range structFields.List {
				name := field.Names[0].Name
				if strings.HasPrefix(name, "XXX_") {
					continue
				}
				typ := ""
				switch field.Type.(type) {
				case *ast.StructType:
					typ = "struct{}"
				case *ast.ArrayType:
					typ = "[]" + ((field.Type.(*ast.ArrayType)).Elt).(*ast.Ident).Name
				case *ast.Ident:
					typ = (field.Type.(*ast.Ident)).Name
				}
				comment := ""
				if field.Comment != nil && len(field.Comment.List) != 0 {
					comment = (field.Comment.List[0]).Text
				}
				info.FieldNum++
				info.Fields = append(info.Fields, structField{
					Name:    name,
					Type:    typ,
					Comment: comment,
				})

			}
		}
		return true
	})
	return info, nil
}
