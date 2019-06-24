package parser

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	template "github.com/yiv/gk/templates"
	"golang.org/x/tools/imports"
)

type File struct {
	Comment    string
	Package    string
	Imports    []NamedTypeValue
	Constants  []NamedTypeValue
	Vars       []NamedTypeValue
	Interfaces []Interface
	Structs    []Struct
	Methods    []Method
	AliasType  []NamedTypeValue
}

func NewFile() File {
	return File{
		Interfaces: []Interface{},
		Imports:    []NamedTypeValue{},
		Structs:    []Struct{},
		Vars:       []NamedTypeValue{},
		Constants:  []NamedTypeValue{},
		Methods:    []Method{},
	}
}

func (f *File) String() string {
	s, err := template.NewEngine().Execute("file", f)
	if err != nil {
		logrus.Panic(err)
	}
	dt, err := imports.Process(f.Package, []byte(s), nil)
	if err != nil {
		logrus.Println("###########################")
		fmt.Printf("%v", s)
		logrus.Println("###########################")
		logrus.Panic(err)
	}
	return string(dt)
}
