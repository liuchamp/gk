package generator

import (
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/yiv/gk/fs"
	"github.com/yiv/gk/parser"
	template "github.com/yiv/gk/templates"
	"github.com/yiv/gk/utils"
	"os"
	"strings"
)

type ThriftInitGenerator struct {
}

func NewThriftInitGenerator() *ThriftInitGenerator {
	return &ThriftInitGenerator{}
}

func (sg *ThriftInitGenerator) Generate(name string) error {
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	fname, err := te.ExecuteString(viper.GetString("service.file_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile := path + defaultFs.FilePathSeparator() + fname
	b, err := defaultFs.Exists(sfile)
	if err != nil {
		return err
	}
	iname, err := te.ExecuteString(viper.GetString("service.interface_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	if !b {
		return errors.New(fmt.Sprintf("Service %s was not found", name))
	}
	p := parser.NewFileParser()
	s, err := defaultFs.ReadFile(sfile)
	if err != nil {
		return err
	}
	f, err := p.Parse([]byte(s))
	if err != nil {
		return err
	}
	var iface *parser.Interface
	for _, v := range f.Interfaces {
		if v.Name == iname {
			iface = &v
		}
	}
	if iface == nil {
		return errors.New(fmt.Sprintf("Could not find the service interface in `%s`", sfile))
	}
	toKeep := []parser.Method{}
	for _, v := range iface.Methods {
		isOk := false
		for _, p := range v.Parameters {
			if p.Type == "context.Context" {
				isOk = true
				break
			}
		}
		if string(v.Name[0]) == strings.ToLower(string(v.Name[0])) {
			logrus.Warnf("The method '%s' is private and will be ignored", v.Name)
			continue
		}
		if len(v.Results) == 0 {
			logrus.Warnf("The method '%s' does not have any return value and will be ignored", v.Name)
			continue
		}
		if !isOk {
			logrus.Warnf("The method '%s' does not have a context and will be ignored", v.Name)
		}
		if isOk {
			toKeep = append(toKeep, v)
		}

	}
	iface.Methods = toKeep
	if len(iface.Methods) == 0 {
		return errors.New("The service has no method please implement the interface methods")
	}
	path, err = te.ExecuteString(viper.GetString("transport.path"), map[string]string{
		"ServiceName":   name,
		"TransportType": "thrift",
	})
	if err != nil {
		return err
	}
	sfile = path + defaultFs.FilePathSeparator() + "gen-go" + defaultFs.FilePathSeparator() +
		utils.ToLowerSnakeCase(name) + defaultFs.FilePathSeparator() +
		utils.ToLowerSnakeCase(name) + ".go"
	b, err = defaultFs.Exists(sfile)
	if err != nil {
		return err
	}
	if !b {
		return errors.New("Could not find the compiled thrift of the service")
	}
	gosrc := utils.GetGOPATH() + "/src/"
	gosrc = strings.Replace(gosrc, "\\", "/", -1)
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if viper.GetString("gk_folder") != "" {
		pwd += "/" + viper.GetString("gk_folder")
	}
	pwd = strings.Replace(pwd, "\\", "/", -1)
	projectPath := strings.Replace(pwd, gosrc, "", 1)
	thriftImport := projectPath + "/" + path + "/" + "gen-go" +
		"/" + utils.ToLowerSnakeCase(name)
	thriftImport = strings.Replace(thriftImport, "\\", "/", -1)
	enpointsPath, err := te.ExecuteString(viper.GetString("endpoints.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	enpointsPath = strings.Replace(enpointsPath, "\\", "/", -1)
	endpointsImport := projectPath + "/" + enpointsPath
	handler := parser.NewFile()
	handler.Package = "thrift"
	handler.Imports = []parser.NamedTypeValue{
		parser.NewNameType("", "\"context\""),
		parser.NewNameType("", "\"errors\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/endpoint\""),
		parser.NewNameType(
			fmt.Sprintf("thrift%s", utils.ToUpperFirstCamelCase(name)),
			fmt.Sprintf("\"%s\"", thriftImport),
		),
		parser.NewNameType("", fmt.Sprintf("\"%s\"", endpointsImport)),
	}
	thriftStruct := parser.NewStruct("thriftServer", []parser.NamedTypeValue{
		parser.NewNameType("ctx", "context.Context"),
	})
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"MakeThriftHandler",
		`MakeThriftHandler makes a set of endpoints available as a thrift server.`,
		parser.NamedTypeValue{},
		`req = &thriftServer{
				ctx:    ctx,`,
		[]parser.NamedTypeValue{
			parser.NewNameType("ctx", "context.Context"),
			parser.NewNameType("endpoints", "endpoints.Endpoints"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("req", fmt.Sprintf("thrift%s.%sService",
				utils.ToUpperFirstCamelCase(name), utils.ToUpperFirstCamelCase(name))),
		},
	))
	for _, v := range iface.Methods {
		thriftStruct.Vars = append(thriftStruct.Vars, parser.NewNameType(
			utils.ToLowerFirstCamelCase(v.Name),
			"endpoint.Endpoint",
		))
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"DecodeThrift"+v.Name+"Request",
			fmt.Sprintf(
				`DecodeThrift%sRequest is a func that converts a
				thrift request to a user-domain request. Primarily useful in a server.
				TODO: Do not forget to implement the decoder.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`err = errors.New("'%s' Decoder is not impelement")
			return req, err`, v.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("r", fmt.Sprintf("*thrift%s.%sRequest",
					utils.ToUpperFirstCamelCase(name), utils.ToUpperFirstCamelCase(v.Name))),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("req", fmt.Sprintf("endpoints.%sRequest",
					utils.ToUpperFirstCamelCase(v.Name))),
				parser.NewNameType("err", "error"),
			},
		))
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"EncodeThrift"+v.Name+"Response",
			fmt.Sprintf(
				`EncodeThrift%sResponse is a func that converts a
					user-domain response to a thrift reply. Primarily useful in a server.
					TODO: Do not forget to implement the encoder.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`err = errors.New("'%s' Encoder is not impelement")
			return rep, err`, v.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("reply", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("rep", fmt.Sprintf("thrift%s.%sReply",
					utils.ToUpperFirstCamelCase(name), utils.ToUpperFirstCamelCase(v.Name))),
				parser.NewNameType("err", "error"),
			},
		))
		handler.Methods = append(handler.Methods, parser.NewMethod(
			v.Name,
			parser.NewNameType("s", "*thriftServer"),
			fmt.Sprintf(
				`request,err:=DecodeThrift%sRequest(req)
					if err != nil {
						return nil, err
					}
					response, err := s.%s(s.ctx, request)
					if err != nil {
						return nil, err
					}
					r,err := EncodeThrift%sResponse(response)
					rep = &r
					return rep, err`,
				utils.ToUpperFirstCamelCase(v.Name),
				utils.ToLowerFirstCamelCase(v.Name),
				utils.ToUpperFirstCamelCase(v.Name),
			),
			[]parser.NamedTypeValue{
				parser.NewNameType("req", fmt.Sprintf("*thrift%s.%sRequest", utils.ToUpperFirstCamelCase(name), utils.ToUpperFirstCamelCase(v.Name))),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("rep", fmt.Sprintf("*thrift%s.%sReply", utils.ToUpperFirstCamelCase(name), utils.ToUpperFirstCamelCase(v.Name))),
				parser.NewNameType("err", "error"),
			},
		))
		handler.Methods[0].Body += "\n" + fmt.Sprintf(`%s :  endpoints.%sEndpoint,`,
			utils.ToLowerFirstCamelCase(v.Name), utils.ToUpperFirstCamelCase(v.Name))
	}
	handler.Methods[0].Body += `
	}
	return req`
	handler.Structs = append(handler.Structs, thriftStruct)
	fname, err = te.ExecuteString(viper.GetString("transport.file_name"), map[string]string{
		"ServiceName":   name,
		"TransportType": "thrift",
	})
	if err != nil {
		return err
	}
	sfile = path + defaultFs.FilePathSeparator() + fname
	err = defaultFs.WriteFile(sfile, handler.String(), false)
	if err != nil {
		return err
	}
	logrus.Warn("---------------------------------------------------------------------------------------")
	logrus.Warn("The generator does not implement the Decoding and Encoding of the thrift request/response")
	logrus.Warn("Before using the service don't forget to implement those.")
	logrus.Warn("---------------------------------------------------------------------------------------")
	return nil
}
