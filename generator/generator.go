package generator

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/go-errors/errors"
	"github.com/spf13/viper"
	"github.com/yiv/gk/fs"
	"github.com/yiv/gk/parser"
	"github.com/yiv/gk/templates"
	"github.com/yiv/gk/utils"
	"golang.org/x/tools/imports"
)

var SUPPORTED_TRANSPORTS = []string{"http", "grpc", "thrift"}

type ServiceGenerator struct {
}

func (sg *ServiceGenerator) Generate(name string) error {
	logrus.Info(fmt.Sprintf("Generating service: %s", name))
	f := parser.NewFile()
	f.Package = fmt.Sprintf("%sservice", name)
	te := template.NewEngine()
	iname, err := te.ExecuteString(viper.GetString("service.interface_name"), map[string]string{
		"ServiceName": name,
	})
	logrus.Debug(fmt.Sprintf("Service interface name : %s", iname))
	if err != nil {
		return err
	}
	f.Interfaces = []parser.Interface{
		parser.NewInterfaceWithComment(iname, `Implement yor service methods methods.
		e.x: Foo(ctx context.Context,bar string)(rs string, err error)`, []parser.Method{}),
	}
	defaultFs := fs.Get()

	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	logrus.Debug(fmt.Sprintf("Service path: %s", path))
	if err != nil {
		return err
	}
	b, err := defaultFs.Exists(path)
	if err != nil {
		return err
	}
	fname, err := te.ExecuteString(viper.GetString("service.file_name"), map[string]string{
		"ServiceName": name,
	})
	logrus.Debug(fmt.Sprintf("Service file name: %s", fname))
	if err != nil {
		return err
	}
	if b {
		logrus.Debug("Service folder already exists")
		return fs.NewDefaultFs(path).WriteFile(fname, f.String(), false)
	}
	err = defaultFs.MkdirAll(path)
	logrus.Debug(fmt.Sprintf("Creating folder structure : %s", path))
	if err != nil {
		return err
	}
	return fs.NewDefaultFs(path).WriteFile(fname, f.String(), false)
}
func NewServiceGenerator() *ServiceGenerator {
	return &ServiceGenerator{}
}

type ServiceInitGenerator struct {
}

func (sg *ServiceInitGenerator) Generate(name string) error {
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
	transport := viper.GetString("gk_transport")
	supported := false
	for _, v := range SUPPORTED_TRANSPORTS {
		if v == transport {
			supported = true
			break
		}
	}
	if !supported {
		return errors.New(fmt.Sprintf("Transport `%s` not supported", transport))
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
		return errors.New("The service has no suitable methods please implement the interface methods")
	}

	stubName, err := te.ExecuteString(viper.GetString("service.struct_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	stub := parser.NewStruct(stubName, []parser.NamedTypeValue{})
	exists := false
	for _, v := range f.Structs {
		if v.Name == stub.Name {
			logrus.Infof("Service `%s` structure already exists so it will not be recreated.", stub.Name)
			exists = true
		}
	}
	if !exists {
		s += "\n" + stub.String()
	}
	exists = false
	for _, v := range f.Methods {
		if v.Name == "New" {
			logrus.Infof("Service `%s` New function already exists so it will not be recreated", stub.Name)
			exists = true
		}
	}
	if !exists {
		newMethod := parser.NewMethodWithComment(
			"NewBasicService",
			`Get a new instance of the service.
			If you want to add service middleware this is the place to put them.`,
			parser.NamedTypeValue{},
			fmt.Sprintf(`svc = %s{}
			return svc,nil`, stub.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("logger", "log.Logger"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("svc", "Service"),
				parser.NewNameType("err", "error"),
			},
		)
		s += "\n" + newMethod.String()
	}
	for _, m := range iface.Methods {
		exists = false
		m.Struct = parser.NewNameType(strings.ToLower(iface.Name[:1]), stub.Name)
		for _, v := range f.Methods {
			if v.Name == m.Name && v.Struct.Type == m.Struct.Type {
				logrus.Infof("Service method `%s` already exists so it will not be recreated.", v.Name)
				exists = true
			}
		}
		m.Comment = fmt.Sprintf(`// Implement the business logic of %s`, m.Name)
		m.Body = fmt.Sprintf("To-do")
		if !exists {
			s += "\n" + m.String()
		}
	}
	d, err := imports.Process("g", []byte(s), nil)
	if err != nil {
		return err
	}
	err = defaultFs.WriteFile(sfile, string(d), true)
	if err != nil {
		return err
	}
	err = sg.generateServiceLoggingMiddleware(name, iface)
	if err != nil {
		return err
	}
	err = sg.generateServiceInstrumentingMiddleware(name, iface)
	if err != nil {
		return err
	}
	err = sg.generateEndpoints(name, iface)
	if err != nil {
		return err
	}
	err = sg.generateTransport(name, iface, transport)
	if err != nil {
		return err
	}
	return nil
}

func (sg *ServiceInitGenerator) generateTransport(name string, iface *parser.Interface, transport string) error {
	switch transport {
	case "http":
		logrus.Info("Selected http transport.")
		return sg.generateHttpTransport(name, iface)
	case "grpc":
		logrus.Info("Selected grpc transport.")
		return sg.generateGRPCTransport(name, iface)
	case "thrift":
		logrus.Info("Selected thrift transport.")
		return sg.generateThriftTransport(name, iface)
	default:
		return errors.New(fmt.Sprintf("Transport `%s` not supported", transport))
	}
}
func (sg *ServiceInitGenerator) generateHttpTransport(name string, iface *parser.Interface) error {
	logrus.Info("Generating http transport...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	handlerFile := parser.NewFile()
	handlerFile.Package = fmt.Sprintf("%stransport", name)
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
	enpointsPath, err := te.ExecuteString(viper.GetString("endpoints.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	enpointsPath = strings.Replace(enpointsPath, "\\", "/", -1)
	endpointsImport := projectPath + "/" + enpointsPath
	handlerFile.Imports = []parser.NamedTypeValue{
		parser.NewNameType("stdopentracing", "\"github.com/opentracing/opentracing-go\"\n"),
		parser.NewNameType("", "\"github.com/go-kit/kit/log\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/auth/jwt\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/tracing/opentracing\""),
		parser.NewNameType("httptransport", "\"github.com/go-kit/kit/transport/http\"\n"),
		parser.NewNameType("", "\""+endpointsImport+"\""),
	}

	handlerFile.Methods = append(handlerFile.Methods,
		parser.NewMethodWithComment(
			"NewHTTPHandler",
			`NewHTTPHandler returns a handler that makes a set of endpoints available on
			 predefined paths.`,
			parser.NamedTypeValue{},
			`m := http.NewServeMux()
			options := []httptransport.ServerOption{
			httptransport.ServerErrorEncoder(errorEncoder),
			httptransport.ServerErrorLogger(logger),
		}
		`,
			[]parser.NamedTypeValue{
				parser.NewNameType("endpoints", fmt.Sprintf("%sendpoint", name)+".Set"),
				parser.NewNameType("tracer", "stdopentracing.Tracer"),
				parser.NewNameType("logger", "log.Logger"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "http.Handler"),
			},
		),
		parser.NewMethodWithComment(
			"encodeHTTPGenericResponse",
			`encodeHTTPGenericResponse`,
			parser.NamedTypeValue{},
			`s, err := json.Marshal(response)
			 d := RC4Crypt(s)
			 w.Write(d)
			 return err`,
			[]parser.NamedTypeValue{
				parser.NewNameType("ctx", "context.Context"),
				parser.NewNameType("w", "http.ResponseWriter"),
				parser.NewNameType("response", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "error"),
			},
		),
		parser.NewMethodWithComment(
			"RC4Crypt",
			`RC4Crypt`,
			parser.NamedTypeValue{},
			`key := []byte("xxxxxxxxxxxxxxxxxxxxxxx")
			 c, _ := rc4.NewCipher(key)
			 d := make([]byte, len(s))
			 c.XORKeyStream(d, s)
			 return d`,
			[]parser.NamedTypeValue{
				parser.NewNameType("s", "[]byte"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "[]byte"),
			},
		),
		parser.NewMethodWithComment(
			"errorEncoder",
			`errorEncoder`,
			parser.NamedTypeValue{},
			`code := http.StatusInternalServerError
			 msg := err.Error()
			 switch err {
			 default:
			 	code = http.StatusInternalServerError
			 }
			 w.WriteHeader(code)
			 s, err := json.Marshal(errorWrapper{Error: msg})
			 d := RC4Crypt(s)
			 w.Write(d)`,
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("err", "error"),
				parser.NewNameType("w", "http.ResponseWriter"),
			},
			[]parser.NamedTypeValue{},
		),
	)
	for _, m := range iface.Methods {
		handlerFile.Methods = append(handlerFile.Methods, parser.NewMethodWithComment(
			fmt.Sprintf("decodeHTTP%sReq", m.Name),
			fmt.Sprintf(`decodeHTTP%sReq is a transport/http.DecodeRequestFunc that decodes a
					 JSON-encoded request from the HTTP request body. Primarily useful in a server.`,
				m.Name),
			parser.NamedTypeValue{},
			fmt.Sprintf(`req := %sendpoint.%sReq{}
			body, _ := ioutil.ReadAll(r.Body)
			err := json.Unmarshal(RC4Crypt(body), &req)
			return req, err`, name, m.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("r", "*http.Request"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))
		//handlerFile.Methods = append(handlerFile.Methods, parser.NewMethodWithComment(
		//	fmt.Sprintf("encodeHTTP%sRes", m.Name),
		//	fmt.Sprintf(`encodeHTTP%sRes is a transport/http.EncodeResponseFunc that encodes
		//		the response as JSON to the response writer. Primarily useful in a server.`, m.Name),
		//	parser.NamedTypeValue{},
		//	` w.Header().Set("Content-Type", "application/json; charset=utf-8")
		//	err = json.NewEncoder(w).Encode(response)
		//	return err`,
		//	[]parser.NamedTypeValue{
		//		parser.NewNameType("_", "context.Context"),
		//		parser.NewNameType("w", "http.ResponseWriter"),
		//		parser.NewNameType("response", "interface{}"),
		//	},
		//	[]parser.NamedTypeValue{
		//		parser.NewNameType("err", "error"),
		//	},
		//))
		handlerFile.Methods[0].Body += "\n" + fmt.Sprintf(`m.Handle("/%s", httptransport.NewServer(
        endpoints.%sEndpoint,
        decodeHTTP%sReq,
        encodeHTTPGenericResponse,
        append(options,
			httptransport.ServerBefore(opentracing.HTTPToContext(tracer, "%s", logger)),
			httptransport.ServerBefore(jwt.HTTPToContext()),
		)...,
    ))`, utils.ToLowerFirstCamelCase(m.Name), m.Name, m.Name, utils.ToLowerFirstCamelCase(m.Name))
	}
	handlerFile.Methods[0].Body += "\n" + "return m"
	path, err := te.ExecuteString(viper.GetString("httptransport.path"), map[string]string{
		"ServiceName":   name,
		"TransportType": "http",
	})
	if err != nil {
		return err
	}
	b, err := defaultFs.Exists(path)
	if err != nil {
		return err
	}
	fname, err := te.ExecuteString(viper.GetString("httptransport.file_name"), map[string]string{
		"ServiceName":   name,
		"TransportType": "http",
	})
	if err != nil {
		return err
	}
	tfile := path + defaultFs.FilePathSeparator() + fname
	if b {
		fex, err := defaultFs.Exists(tfile)
		if err != nil {
			return err
		}
		if fex {
			logrus.Errorf("Transport for service `%s` exist", name)
			logrus.Info("If you are trying to update a service use `gk update service [serviceName]`")
			return nil
		}
	} else {
		err = defaultFs.MkdirAll(path)
		if err != nil {
			return err
		}
	}
	return defaultFs.WriteFile(tfile, handlerFile.String(), false)
}
func (sg *ServiceInitGenerator) generateGRPCTransport(name string, iface *parser.Interface) error {
	logrus.Info("Generating grpc transport...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	model := map[string]interface{}{
		"Name":    utils.ToUpperFirstCamelCase(name),
		"Methods": []map[string]string{},
	}
	mthds := []map[string]string{}
	for _, v := range iface.Methods {
		mthds = append(mthds, map[string]string{
			"Name":    v.Name,
			"Request": v.Name + "Req",
			"Reply":   v.Name + "Res",
		})
	}
	model["Methods"] = mthds
	path, err := te.ExecuteString(viper.GetString("pb.path"), map[string]string{
		"ServiceName": name,
		//"TransportType": "grpc",
	})
	//path += defaultFs.FilePathSeparator() + "pb"
	//if err != nil {
	//	return err
	//}
	b, err := defaultFs.Exists(path)
	if err != nil {
		return err
	}
	fname := utils.ToLowerSnakeCase(name)
	tfile := path + defaultFs.FilePathSeparator() + fname + ".proto"
	if b {
		fex, err := defaultFs.Exists(tfile)
		if err != nil {
			return err
		}
		if fex {
			logrus.Errorf("Proto for service `%s` exist", name)
			return nil
		}
	} else {
		err = defaultFs.MkdirAll(path)
		if err != nil {
			return err
		}
	}
	protoTmpl, err := te.Execute("proto.pb", model)
	if err != nil {
		return err
	}
	err = defaultFs.WriteFile(tfile, protoTmpl, false)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		tfile := path + defaultFs.FilePathSeparator() + "compile.bat"
		cmpTmpl, err := te.Execute("proto_compile.bat", map[string]string{
			"Name": fname,
		})
		if err != nil {
			return err
		}
		logrus.Warn("--------------------------------------------------------------------")
		logrus.Warn("The service is still not ready!!")
		logrus.Warn("To create the grpc transport please create your protobuf.")
		logrus.Warn("Than follow the instructions in compile.bat and compile the .proto file.")
		logrus.Warnf("After the file is compiled run `gk init grpc %s`.", name)
		logrus.Warn("--------------------------------------------------------------------")
		return defaultFs.WriteFile(tfile, cmpTmpl, false)
	} else {
		tfile := path + defaultFs.FilePathSeparator() + "compile.sh"
		cmpTmpl, err := te.Execute("proto_compile.sh", map[string]string{
			"Name": fname,
		})
		if err != nil {
			return err
		}
		logrus.Warn("--------------------------------------------------------------------")
		logrus.Warn("The service is still not ready!!")
		logrus.Warn("To create the grpc transport please create your protobuf.")
		logrus.Warn("Than follow the instructions in compile.sh and compile the .proto file.")
		logrus.Warnf("After the file is compiled run `gk init grpc %s`.", name)
		logrus.Warn("--------------------------------------------------------------------")
		return defaultFs.WriteFile(tfile, cmpTmpl, false)
	}
}
func (sg *ServiceInitGenerator) generateThriftTransport(name string, iface *parser.Interface) error {
	logrus.Info("Generating thrift transport...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	model := map[string]interface{}{
		"Name":    utils.ToUpperFirstCamelCase(name),
		"Methods": []map[string]string{},
	}
	mthds := []map[string]string{}
	for _, v := range iface.Methods {
		mthds = append(mthds, map[string]string{
			"Name":    v.Name,
			"Request": v.Name + "Request",
			"Reply":   v.Name + "Reply",
		})
	}
	model["Methods"] = mthds
	path, err := te.ExecuteString(viper.GetString("transport.path"), map[string]string{
		"ServiceName":   name,
		"TransportType": "thrift",
	})
	if err != nil {
		return err
	}
	b, err := defaultFs.Exists(path)
	if err != nil {
		return err
	}
	fname := utils.ToLowerSnakeCase(name)
	tfile := path + defaultFs.FilePathSeparator() + fname + ".thrift"
	if b {
		fex, err := defaultFs.Exists(tfile)
		if err != nil {
			return err
		}
		if fex {
			logrus.Errorf("Thrift for service `%s` exist", name)
			return nil
		}
	} else {
		err = defaultFs.MkdirAll(path)
		if err != nil {
			return err
		}
	}
	protoTmpl, err := te.Execute("svc.thrift", model)
	if err != nil {
		return err
	}
	err = defaultFs.WriteFile(tfile, protoTmpl, false)
	if err != nil {
		return err
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
	if err != nil {
		return err
	}
	pkg := strings.Replace(path, "\\", "/", -1)
	pkg = projectPath + "/" + pkg
	if runtime.GOOS == "windows" {
		tfile := path + defaultFs.FilePathSeparator() + "compile.bat"
		cmpTmpl, err := te.Execute("thrift_compile.bat", map[string]string{
			"Name":    fname,
			"Package": pkg,
		})
		if err != nil {
			return err
		}
		logrus.Warn("--------------------------------------------------------------------")
		logrus.Warn("The service is still not ready!!")
		logrus.Warn("To create the thrift transport please create your thrift file.")
		logrus.Warn("Than follow the instructions in compile.bat and compile the .thrift file.")
		logrus.Warnf("After the file is compiled run `gk init thrift %s`.", name)
		logrus.Warn("--------------------------------------------------------------------")
		return defaultFs.WriteFile(tfile, cmpTmpl, false)
	} else {
		tfile := path + defaultFs.FilePathSeparator() + "compile.sh"
		cmpTmpl, err := te.Execute("thrift_compile.sh", map[string]string{
			"Name":    fname,
			"Package": pkg,
		})
		if err != nil {
			return err
		}
		logrus.Warn("--------------------------------------------------------------------")
		logrus.Warn("The service is still not ready!!")
		logrus.Warn("To create the thrift transport please create your thrift file.")
		logrus.Warn("Than follow the instructions in compile.sh and compile the .thrift file.")
		logrus.Warnf("After the file is compiled run `gk init thrift %s`.", name)
		logrus.Warn("--------------------------------------------------------------------")
		return defaultFs.WriteFile(tfile, cmpTmpl, false)
	}
}
func (sg *ServiceInitGenerator) generateEndpoints(name string, iface *parser.Interface) error {
	logrus.Info("Generating endpoints...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	enpointsPath, err := te.ExecuteString(viper.GetString("endpoints.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	b, err := defaultFs.Exists(enpointsPath)
	if err != nil {
		return err
	}
	endpointsFileName, err := te.ExecuteString(viper.GetString("endpoints.file_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	eFile := enpointsPath + defaultFs.FilePathSeparator() + endpointsFileName
	if b {
		fex, err := defaultFs.Exists(eFile)
		if err != nil {
			return err
		}
		if fex {
			logrus.Errorf("Endpoints for service `%s` exist", name)
			logrus.Info("If you are trying to add functions to a service use `gk update service [serviceName]`")
			return nil
		}
	} else {
		err = defaultFs.MkdirAll(enpointsPath)
		if err != nil {
			return err
		}
	}
	file := parser.NewFile()
	// add package name
	file.Package = fmt.Sprintf("%sendpoint", name)
	// add endpoint set struct
	file.Structs = []parser.Struct{
		parser.NewStructWithComment(
			"Set",
			`Set collects all of the endpoints that compose an add service. It's
				meant to be used as a helper struct, to collect all of the endpoints into a
				single parameter.`,
			[]parser.NamedTypeValue{}),
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
	servicePath, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	servicePath = strings.Replace(servicePath, "\\", "/", -1)
	serviceImport := projectPath + "/" + servicePath

	//add import
	file.Imports = []parser.NamedTypeValue{
		parser.NewNameType("stdopentracing", "\"github.com/opentracing/opentracing-go\""),
		parser.NewNameType("stdjwt", "\"github.com/dgrijalva/jwt-go\"\n"),
		parser.NewNameType("", "\"github.com/go-kit/kit/log\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/tracing/opentracing\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/metrics\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/auth/jwt\"\n"),
		parser.NewNameType("", "\""+serviceImport+"\"\n"),
	}

	file.Interfaces = []parser.Interface{
		parser.NewInterfaceWithComment("Failer",
			`Failer is an interface that should be implemented by response types.
 			 Response encoders can check if responses are Failer, and if so if they've
 			 failed, and if so encode them using a separate write path based on the error.
			`,
			[]parser.Method{
				parser.NewMethod("Failed", parser.NamedTypeValue{}, "", []parser.NamedTypeValue{}, []parser.NamedTypeValue{}),
			}),
	}

	//add set create method
	file.Methods = []parser.Method{
		parser.NewMethod(
			"New",
			parser.NamedTypeValue{},
			fmt.Sprintf(`
			kf := func(token *stdjwt.Token) (interface{}, error) {
				return []byte(%sservice.JwtHmacSecret), nil
			}
			claimsFactory := func() stdjwt.Claims {
				return &stdjwt.MapClaims{}
			}`, name),
			[]parser.NamedTypeValue{
				parser.NewNameType("svc", fmt.Sprintf("%sservice", name)+"."+iface.Name),
				parser.NewNameType("logger", "log.Logger"),
				parser.NewNameType("duration", "metrics.Histogram"),
				parser.NewNameType("trace", "stdopentracing.Tracer"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("set", "Set"),
			},
		),
	}

	for _, v := range iface.Methods {
		file.Structs[0].Vars = append(file.Structs[0].Vars, parser.NewNameType(v.Name+"Endpoint", "endpoint.Endpoint"))
		reqPrams := []parser.NamedTypeValue{}
		for _, p := range v.Parameters {
			if p.Type != "context.Context" {
				n := strings.ToUpper(string(p.Name[0])) + p.Name[1:]
				reqPrams = append(reqPrams, parser.NewNameType(n, p.Type))
			}
		}
		resultPrams := []parser.NamedTypeValue{}
		for _, p := range v.Results {
			n := strings.ToUpper(string(p.Name[0])) + p.Name[1:]
			resultPrams = append(resultPrams, parser.NewNameType(n, p.Type))
		}
		req := parser.NewStruct(v.Name+"Req", reqPrams)
		res := parser.NewStruct(v.Name+"Res", resultPrams)
		file.Structs = append(file.Structs, req)
		file.Structs = append(file.Structs, res)

		//add Failer interface method for response
		file.Methods = append(file.Methods, parser.NewMethod(
			"Failed",
			parser.NewNameType("r", v.Name+"Res"),
			fmt.Sprintf(`return r.Err`),
			[]parser.NamedTypeValue{},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "error"),
			},
		))

		tmplModel := map[string]interface{}{
			"Calling":  v,
			"Request":  req,
			"Response": res,
		}
		tRes, err := te.ExecuteString("{{template \"endpoint_func\" .}}", tmplModel)
		if err != nil {
			return err
		}

		// add endpoint maker method
		file.Methods = append(file.Methods, parser.NewMethodWithComment(
			"Make"+v.Name+"Endpoint",
			fmt.Sprintf(`Make%sEndpoint returns an endpoint that invokes %s on the service.
				  Primarily useful in a server.`, v.Name, v.Name),
			parser.NamedTypeValue{},
			tRes,
			[]parser.NamedTypeValue{
				parser.NewNameType("svc", fmt.Sprintf("%sservice", name)+"."+iface.Name),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("ep", "endpoint.Endpoint"),
			},
		))
		//add interface method for set of endpoints
		file.Methods = append(file.Methods, parser.NewMethod(
			v.Name,
			parser.NewNameType("s", "Set"),
			fmt.Sprintf(`
			request := %sReq{To-do}
			resp, err := s.%sEndpoint(ctx, request)
			if err != nil {
				return nil, err
			}
			response := resp.(%sRes)
			return response.Value, response.Err
			`, v.Name,
				utils.ToUpperFirstCamelCase(v.Name),
				utils.ToUpperFirstCamelCase(v.Name)),
			v.Parameters,
			v.Results,
		))

		file.Methods[0].Body += fmt.Sprintf(`
		var %sEndpoint endpoint.Endpoint
		{
			method := "%s"
			%sEndpoint = Make%sEndpoint(svc)
			%sEndpoint = opentracing.TraceServer(trace, method)(%sEndpoint)
			%sEndpoint = LoggingMiddleware(log.With(logger, "method", method))(%sEndpoint)
			%sEndpoint = InstrumentingMiddleware(duration.With("method", method))(%sEndpoint)
			%sEndpoint = jwt.NewParser(kf, stdjwt.SigningMethodHS256, claimsFactory)(%sEndpoint)
			set.%sEndpoint = %sEndpoint
		}
		`, utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToUpperFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToUpperFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name))
	}
	file.Methods[0].Body += "\n return set"

	err = defaultFs.WriteFile(eFile, file.String(), false)
	if err != nil {
		return err
	}

	err = sg.generateEndpointsMiddleware(name, iface)
	if err != nil {
		return err
	}
	return nil
}

func (sg *ServiceInitGenerator) generateEndpointsMiddleware(name string, iface *parser.Interface) error {
	logrus.Info("Generating endpoints middleware...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	enpointsPath, err := te.ExecuteString(viper.GetString("endpoints.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	eFile := enpointsPath + defaultFs.FilePathSeparator() + "middleware.go"

	file := parser.NewFile()
	file.Package = fmt.Sprintf("%sendpoint", name)

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
	servicePath, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	servicePath = strings.Replace(servicePath, "\\", "/", -1)
	serviceImport := projectPath + "/" + servicePath
	file.Imports = []parser.NamedTypeValue{
		parser.NewNameType("", "\"github.com/go-kit/kit/log\"\n"),
		parser.NewNameType("", "\""+serviceImport+"\""),
	}

	file.Methods = append(file.Methods, parser.NewMethodWithComment(
		"InstrumentingMiddleware",
		fmt.Sprintf(`
		InstrumentingMiddleware returns an endpoint middleware that records
 		the duration of each invocation to the passed histogram. The middleware adds
 		a single field: "success", which is "true" if no error is returned, and
 		"false" otherwise.
		`),
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		return func(next endpoint.Endpoint) endpoint.Endpoint {
			return func(ctx context.Context, request interface{}) (response interface{}, err error) {
				defer func(begin time.Time) {
					duration.With("success", fmt.Sprint(err == nil)).Observe(time.Since(begin).Seconds())
				}(time.Now())
				return next(ctx, request)

			}
		}
		`),
		[]parser.NamedTypeValue{
			parser.NewNameType("duration", "metrics.Histogram"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "endpoint.Middleware"),
		},
	))
	file.Methods = append(file.Methods, parser.NewMethodWithComment(
		"LoggingMiddleware",
		fmt.Sprintf(`
		LoggingMiddleware returns an endpoint middleware that logs the
 		duration of each invocation, and the resulting error, if any.
		`),
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		return func(next endpoint.Endpoint) endpoint.Endpoint {
			return func(ctx context.Context, request interface{}) (response interface{}, err error) {
				defer func(begin time.Time) {
					logger.Log("transport_error", err, "took", time.Since(begin))
				}(time.Now())
				return next(ctx, request)

			}
		}
		`),
		[]parser.NamedTypeValue{
			parser.NewNameType("logger", "log.Logger"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "endpoint.Middleware"),
		},
	))
	return defaultFs.WriteFile(eFile, file.String(), false)
}
func (sg *ServiceInitGenerator) generateServiceInstrumentingMiddleware(name string, iface *parser.Interface) error {
	logrus.Info("Generating service instrumenting middleware...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile := path + defaultFs.FilePathSeparator() + "instrumenting.go"

	file := parser.NewFile()
	file.Package = fmt.Sprintf("%sservice", name)

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
	servicePath, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	servicePath = strings.Replace(servicePath, "\\", "/", -1)
	serviceImport := projectPath + "/" + servicePath
	file.Imports = []parser.NamedTypeValue{
		parser.NewNameType("", "\"github.com/go-kit/kit/log\"\n"),
		parser.NewNameType("", "\""+serviceImport+"\""),
	}

	file.Structs = []parser.Struct{
		parser.NewStructWithComment(
			"instrumentingMiddleware",
			``,
			[]parser.NamedTypeValue{
				parser.NewNameType("requestCount", "metrics.Counter"),
				parser.NewNameType("requestLatency", "metrics.Histogram"),
				parser.NewNameType("next", "Service"),
			}),
	}
	file.Methods = append(file.Methods, parser.NewMethodWithComment(
		"InstrumentingMiddleware",
		fmt.Sprintf(`
		InstrumentingMiddleware returns a service middleware that instruments
 		the number of integers summed and characters concatenated over the lifetime of
 		the service.`),
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		return func(next Service) Service {
			return instrumentingMiddleware{
				requestCount:   requestCount,
				requestLatency: requestLatency,
				next:           next,
			}
		}`),
		[]parser.NamedTypeValue{
			parser.NewNameType("requestCount", "metrics.Counter"),
			parser.NewNameType("requestLatency", "metrics.Histogram"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "Middleware"),
		},
	))

	for _, v := range iface.Methods {
		var retBody string
		for i, p := range v.Parameters {
			retBody += p.Name
			if i < len(v.Parameters)-1 {
				retBody += ","
			}
		}
		file.Methods = append(file.Methods,
			parser.NewMethod(
				fmt.Sprintf("%s", v.Name),
				parser.NewNameType("mw", "instrumentingMiddleware"),
				fmt.Sprintf(`
				defer func(begin time.Time) {
					lvs := []string{"method", "%s", "error", fmt.Sprint(err != nil)}
					mw.requestCount.With(lvs...).Add(1)
					mw.requestLatency.With(lvs...).Observe(time.Since(begin).Seconds())
				}(time.Now())
				return mw.next.%s(%s)
				`, v.Name, v.Name, retBody),
				v.Parameters,
				v.Results,
			),
		)
	}
	return defaultFs.WriteFile(sfile, file.String(), false)
}

func (sg *ServiceInitGenerator) generateServiceLoggingMiddleware(name string, iface *parser.Interface) error {
	logrus.Info("Generating service logging middleware...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile := path + defaultFs.FilePathSeparator() + "logging.go"

	file := parser.NewFile()
	file.Package = fmt.Sprintf("%sservice", name)

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
	servicePath, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	servicePath = strings.Replace(servicePath, "\\", "/", -1)
	serviceImport := projectPath + "/" + servicePath
	file.Imports = []parser.NamedTypeValue{
		parser.NewNameType("", "\"github.com/go-kit/kit/log\"\n"),
		parser.NewNameType("", "\""+serviceImport+"\""),
	}

	file.Structs = []parser.Struct{
		parser.NewStructWithComment(
			"loggingMiddleware",
			"",
			[]parser.NamedTypeValue{
				parser.NewNameType("logger", "log.Logger"),
				parser.NewNameType("next", "Service"),
			}),
	}
	file.Methods = append(file.Methods, parser.NewMethodWithComment(
		"LoggingMiddleware",
		fmt.Sprintf(`
		LoggingMiddleware takes a logger as a dependency
 		and returns a ServiceMiddleware.`),
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		return func(next Service) Service {
			return loggingMiddleware{logger, next}
		}`),
		[]parser.NamedTypeValue{
			parser.NewNameType("logger", "log.Logger"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "Middleware"),
		},
	))

	for _, v := range iface.Methods {
		var retBody string
		logBody := fmt.Sprintf(`"method", "%s"`, v.Name)
		for i, p := range v.Parameters {
			retBody += p.Name
			if i < len(v.Parameters)-1 {
				retBody += ","
			}
			if p.Name != "ctx" {
				logBody = fmt.Sprintf(`%s, "%s", %s`, logBody, p.Name, p.Name)
			}
		}
		for _, p := range v.Results {
			logBody = fmt.Sprintf(`%s, "%s", %s`, logBody, p.Name, p.Name)
		}
		file.Methods = append(file.Methods,
			parser.NewMethod(
				fmt.Sprintf("%s", v.Name),
				parser.NewNameType("mw", "loggingMiddleware"),
				fmt.Sprintf(`
				defer func() {
					mw.logger.Log(%s)
				}()
				return mw.next.%s(%s)
				`, logBody, v.Name, retBody),
				v.Parameters,
				v.Results,
			),
		)
	}
	return defaultFs.WriteFile(sfile, file.String(), false)
}
func NewServiceInitGenerator() *ServiceInitGenerator {
	return &ServiceInitGenerator{}
}

type GRPCInitGenerator struct {
}

func (sg *GRPCInitGenerator) Generate(name string) error {
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
	path, err = te.ExecuteString(viper.GetString("pb.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile = path + defaultFs.FilePathSeparator() + utils.ToLowerSnakeCase(name) + ".pb.go"
	b, err = defaultFs.Exists(sfile)
	if err != nil {
		return err
	}
	if !b {
		return errors.New("Could not find the compiled pb of the service")
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
	pbImport := projectPath + "/" + path
	pbImport = strings.Replace(pbImport, "\\", "/", -1)
	enpointsPath, err := te.ExecuteString(viper.GetString("endpoints.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	enpointsPath = strings.Replace(enpointsPath, "\\", "/", -1)
	endpointsImport := projectPath + "/" + enpointsPath
	handler := parser.NewFile()
	handler.Package = fmt.Sprintf("%stransport", name)
	handler.Imports = []parser.NamedTypeValue{
		parser.NewNameType("", "\"context\""),
		parser.NewNameType("", "\"errors\"\n"),
		parser.NewNameType("stdopentracing", "\"github.com/opentracing/opentracing-go\""),
		parser.NewNameType("", "\"google.golang.org/grpc\""),
		parser.NewNameType("jujuratelimit", "\"github.com/juju/ratelimit\""),
		parser.NewNameType("oldcontext", "\"golang.org/x/net/context\"\n"),
		parser.NewNameType("grpctransport", "\"github.com/go-kit/kit/transport/grpc\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/ratelimit\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/tracing/opentracing\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/endpoint\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/auth/jwt\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/log\"\n"),
		parser.NewNameType("", fmt.Sprintf("\"%s\"", pbImport)),
		parser.NewNameType("", fmt.Sprintf("\"%s\"", endpointsImport)),
	}
	grpcStruct := parser.NewStruct("grpcServer", []parser.NamedTypeValue{})
	//NewGRPCServer
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"NewGRPCServer",
		`NewGRPCServer makes a set of endpoints available as a gRPC server.`,
		parser.NamedTypeValue{},
		`options := []grpctransport.ServerOption{
			grpctransport.ServerErrorLogger(logger),
		}
		grpcServer := &grpcServer{`,
		[]parser.NamedTypeValue{
			parser.NewNameType("endpoints", fmt.Sprintf("%sendpoint.Set", name)),
			parser.NewNameType("tracer", "stdopentracing.Tracer"),
			parser.NewNameType("logger", "log.Logger"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", fmt.Sprintf("pb.%sServer", utils.ToUpperFirstCamelCase(name))),
		},
	))
	//NewGRPCClient
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"NewGRPCClient",
		`NewGRPCClient makes a set of endpoints available as a gRPC client.`,
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		set := %sendpoint.Set{}
		limiter := ratelimit.NewTokenBucketLimiter(jujuratelimit.NewBucketWithRate(100,100))
		`, name),
		[]parser.NamedTypeValue{
			parser.NewNameType("conn", "*grpc.ClientConn"),
			parser.NewNameType("tracer", "stdopentracing.Tracer"),
			parser.NewNameType("logger", "log.Logger"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", fmt.Sprintf("%sservice.Service", utils.ToLowerFirstCamelCase(name))),
		},
	))
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"str2err",
		`str2err `,
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		if s == "" {
			return nil
		}
		return errors.New(s)
		`),
		[]parser.NamedTypeValue{
			parser.NewNameType("s", "string"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "error"),
		},
	))
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"err2str",
		`err2str `,
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		if err == nil {
			return ""
		}
		return err.Error()
		`),
		[]parser.NamedTypeValue{
			parser.NewNameType("err", "error"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "string"),
		},
	))
	for _, v := range iface.Methods {
		//add member to grpcServer
		grpcStruct.Vars = append(grpcStruct.Vars, parser.NewNameType(
			utils.ToLowerFirstCamelCase(v.Name),
			"grpctransport.Handler",
		))
		// add server side request decoder
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"decodeGRPC"+v.Name+"Req",
			fmt.Sprintf(
				`DecodeGRPC%sRequest is a transport/grpc.DecodeRequestFunc that converts a
				gRPC request to a user-domain request. Primarily useful in a server.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`r := grpcReq.(*pb.%sReq)
			req := %sendpoint.%sReq{To-do}
			return req, nil`, v.Name, name, v.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("grpcReq", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))
		// add server side response encoder
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"encodeGRPC"+v.Name+"Res",
			fmt.Sprintf(
				`EncodeGRPC%sResponse is a transport/grpc.EncodeResponseFunc that converts a
					user-domain response to a gRPC reply. Primarily useful in a server.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`r := response.(%sendpoint.%sRes)
			res := &pb.%sRes{To-do}
			To-do
			return res, nil`, name, v.Name, v.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("response", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))
		// add client side request encoder
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"encodeGRPC"+v.Name+"Req",
			fmt.Sprintf(
				`encodeGRPC%Req s a transport/grpc.EncodeRequestFunc that converts a
				 user-domain sum request to a gRPC sum request. Primarily useful in a client.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`r := request.(%sendpoint.%sReq)
			req :=  &pb.%sReq{To-do}
			return req, nil`, name, v.Name, v.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("request", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))
		// add client side response decoder
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"decodeGRPC"+v.Name+"Res",
			fmt.Sprintf(
				`decodeGRPC%Res is a transport/grpc.DecodeResponseFunc that converts a
				 gRPC sum reply to a user-domain sum response. Primarily useful in a client.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`r := grpcReply.(*pb.%sRes)
			res := %sendpoint.%sRes{To-do}
			return res, nil`, utils.ToUpperFirstCamelCase(v.Name), name, v.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("grpcReply", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))
		//add interface method
		handler.Methods = append(handler.Methods, parser.NewMethod(
			v.Name,
			parser.NewNameType("s", "*grpcServer"),
			fmt.Sprintf(
				`_, rp, err := s.%s.ServeGRPC(ctx, req)
					if err != nil {
						return nil, err
					}
					rep = rp.(*pb.%sRes)
					return rep, err`,
				utils.ToLowerFirstCamelCase(v.Name),
				v.Name,
			),
			[]parser.NamedTypeValue{
				parser.NewNameType("ctx", "oldcontext.Context"),
				parser.NewNameType("req", fmt.Sprintf("*pb.%sReq", v.Name)),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("rep", fmt.Sprintf("*pb.%sRes", v.Name)),
				parser.NewNameType("err", "error"),
			},
		))
		//init grpcServer method
		handler.Methods[0].Body += "\n" + fmt.Sprintf(`%s : grpctransport.NewServer(
			endpoints.%sEndpoint,
			decodeGRPC%sReq,
			encodeGRPC%sRes,
			append(
				options,
				grpctransport.ServerBefore(opentracing.GRPCToContext(tracer, "%s", logger)),
				grpctransport.ServerBefore(jwt.GRPCToContext()),
			)...,
		),
		`, utils.ToLowerFirstCamelCase(v.Name), v.Name, v.Name, v.Name, v.Name)
		//init grpcServer method
		handler.Methods[1].Body += "\n" + fmt.Sprintf(`
			var %sEndpoint endpoint.Endpoint
			{
				%sEndpoint = grpctransport.NewClient(
					conn,
					"pb.%s",
					"%s",
					encodeGRPC%sReq,
					decodeGRPC%sRes,
					pb.%sRes{},
					grpctransport.ClientBefore(opentracing.ContextToGRPC(tracer, logger)),
					grpctransport.ClientBefore(jwt.ContextToGRPC()),
				).Endpoint()
				%sEndpoint = opentracing.TraceClient(tracer, "%s")(%sEndpoint)
				%sEndpoint = limiter(%sEndpoint)
				set.%sEndpoint = %sEndpoint
			}
		`, utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToUpperFirstCamelCase(name),
			v.Name, v.Name, v.Name, v.Name,
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			utils.ToLowerFirstCamelCase(v.Name),
			v.Name,
			utils.ToLowerFirstCamelCase(v.Name))
	}
	//close NewGRPCServer
	handler.Methods[0].Body += `}
	return grpcServer`
	//close NewGRPCClient
	handler.Methods[1].Body += `
	return set`

	handler.Structs = append(handler.Structs, grpcStruct)
	path, err = te.ExecuteString(viper.GetString("grpctransport.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	fname, err = te.ExecuteString(viper.GetString("grpctransport.file_name"), map[string]string{
		"ServiceName": name,
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
	logrus.Warn("The generator does not implement the Decoding and Encoding of the grpc request/response")
	logrus.Warn("Before using the service don't forget to implement those.")
	logrus.Warn("---------------------------------------------------------------------------------------")
	return nil
}
func NewGRPCInitGenerator() *GRPCInitGenerator {
	return &GRPCInitGenerator{}
}

type ThriftInitGenerator struct {
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
func NewThriftInitGenerator() *ThriftInitGenerator {
	return &ThriftInitGenerator{}
}

type AddGRPCGenerator struct {
}

func (sg *AddGRPCGenerator) Generate(name string) error {
	g := NewServiceInitGenerator()
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
	return g.generateGRPCTransport(name, iface)
}
func NewAddGRPCGenerator() *AddGRPCGenerator {
	return &AddGRPCGenerator{}
}

type AddHttpGenerator struct {
}

func (sg *AddHttpGenerator) Generate(name string) error {
	g := NewServiceInitGenerator()
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
	return g.generateHttpTransport(name, iface)
}
func NewAddHttpGenerator() *AddHttpGenerator {
	return &AddHttpGenerator{}
}

type AddThriftGenerator struct {
}

func (sg *AddThriftGenerator) Generate(name string) error {
	g := NewServiceInitGenerator()
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
	return g.generateThriftTransport(name, iface)
}
func NewAddThriftGenerator() *AddThriftGenerator {
	return &AddThriftGenerator{}
}
