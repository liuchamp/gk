package parser

import (
	"github.com/Sirupsen/logrus"
	template "github.com/yiv/gk/templates"
	"github.com/yiv/gk/utils"
	"go/format"
)

type Struct struct {
	Name    string
	Comment string
	Vars    []NamedTypeValue
}

func NewStruct(name string, vars []NamedTypeValue) Struct {
	for k, v := range vars {
		vars[k].Comment = utils.ToLowerSnakeCase(v.Name)
	}
	return Struct{
		Name:    name,
		Comment: "",
		Vars:    vars,
	}
}
func NewStructWithComment(name string, comment string, vars []NamedTypeValue) Struct {
	s := NewStruct(name, vars)
	s.Comment = prepareComments(comment)
	return s
}

func (s *Struct) String() string {
	str, err := template.NewEngine().ExecuteString("{{template \"struct\" .}}", s)
	if err != nil {
		logrus.Panic(err)
	}
	dt, err := format.Source([]byte(str))
	if err != nil {
		logrus.Panic(err)
	}
	return string(dt)
}
