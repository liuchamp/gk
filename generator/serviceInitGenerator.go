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
	"golang.org/x/tools/imports"
	"os"
	"runtime"
	"strings"
)

type ServiceInitGenerator struct {
}

func NewServiceInitGenerator() *ServiceInitGenerator {
	return &ServiceInitGenerator{}
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
	stub := parser.NewStructWithComment(stubName,
		"the concrete implementation of service interface",
		[]parser.NamedTypeValue{parser.NewNameType("Logger", "log.Logger")},
	)
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
			fmt.Sprintf(`
			var  err error
			svc = %s{Logger:logger}
			defer func() {
				if err != nil {
					panic(err.Error())
				}
			}()
			return `, stub.Name),
			[]parser.NamedTypeValue{
				parser.NewNameType("logger", "log.Logger"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("svc", "Service"),
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
		if err := sg.generateHttpTransport(name, iface); err != nil {
			return err
		}
		if err := sg.generateHttpTransportTesting(name, iface); err != nil {
			return err
		}
		return nil
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
		parser.NewNameType("stdzipkin", `"github.com/openzipkin/zipkin-go"`),
		parser.NewNameType("stdopentracing", "\"github.com/opentracing/opentracing-go\"\n"),
		parser.NewNameType("", "\"github.com/go-kit/kit/log\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/auth/jwt\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/tracing/opentracing\""),
		parser.NewNameType("httptransport", "\"github.com/go-kit/kit/transport/http\"\n"),
		parser.NewNameType("", "\""+endpointsImport+"\""),
	}

	handlerFile.Structs = []parser.Struct{
		parser.NewStructWithComment(
			"errorWrapper",
			``,
			[]parser.NamedTypeValue{
				parser.NewNameType("Err", "string"),
			}),
	}

	handlerFile.Methods = append(handlerFile.Methods,
		parser.NewMethodWithComment(
			"NewHTTPHandler",
			`NewHTTPHandler returns a handler that makes a set of endpoints available on
			 predefined paths.`,
			parser.NamedTypeValue{},
			`
			zipkinServer := zipkin.HTTPServerTrace(zipkinTracer)
			options := []httptransport.ServerOption{
				httptransport.ServerErrorEncoder(errorEncoder),
				httptransport.ServerErrorHandler(transport.NewLogErrorHandler(logger)),
				zipkinServer,
			}

			m := http.NewServeMux()`,
			[]parser.NamedTypeValue{
				parser.NewNameType("endpoints", fmt.Sprintf("%sendpoint", name)+".Set"),
				parser.NewNameType("otTracer", "stdopentracing.Tracer"),
				parser.NewNameType("zipkinTracer", "*stdzipkin.Tracer"),
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
			fmt.Sprintf(`if f, ok := response.(%sendpoint.Failer); ok && f.Failed() != nil {
				errorEncoder(ctx, f.Failed(), w)
				return nil
			}
			s, err := json.Marshal(response)
			w.Write(s)
			return err`, name),
			[]parser.NamedTypeValue{
				parser.NewNameType("ctx", "context.Context"),
				parser.NewNameType("w", "http.ResponseWriter"),
				parser.NewNameType("response", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "error"),
			},
		),
		//parser.NewMethodWithComment(
		//	"RC4Crypt",
		//	`RC4Crypt`,
		//	parser.NamedTypeValue{},
		//	`key := []byte("xxxxxxxxxxxxxxxxxxxxxxx")
		//	 c, _ := rc4.NewCipher(key)
		//	 d := make([]byte, len(s))
		//	 c.XORKeyStream(d, s)
		//	 return d`,
		//	[]parser.NamedTypeValue{
		//		parser.NewNameType("s", "[]byte"),
		//	},
		//	[]parser.NamedTypeValue{
		//		parser.NewNameType("", "[]byte"),
		//	},
		//),
		parser.NewMethodWithComment(
			"errorEncoder",
			`errorEncoder`,
			parser.NamedTypeValue{},
			`code := http.StatusInternalServerError
			 msg := err.Error()
			 switch msg {
			 case ErrJWTExpired.Error():
				code = http.StatusUnauthorized
			 default:
			 	code = http.StatusInternalServerError
			 }
			 w.WriteHeader(code)
			 s, err := json.Marshal(errorWrapper{Err: msg})
			 w.Write(s)`,
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
			err := json.Unmarshal(body, &req)
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
			httptransport.ServerBefore(opentracing.HTTPToContext(otTracer, "%s", logger)), 
			httptransport.ServerBefore(jwt.HTTPToContext()),
			)...,
		))`,
			utils.ToLowerHyphenCase(m.Name), m.Name, m.Name, utils.ToLowerFirstCamelCase(m.Name))
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
func (sg *ServiceInitGenerator) generateHttpTransportTesting(name string, iface *parser.Interface) error {
	logrus.Info("Generating http transport testing...")
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
		parser.NewNameType("stdzipkin", `"github.com/openzipkin/zipkin-go"`),
		parser.NewNameType("stdopentracing", "\"github.com/opentracing/opentracing-go\"\n"),
		parser.NewNameType("", "\"github.com/go-kit/kit/log\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/auth/jwt\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/tracing/opentracing\""),
		parser.NewNameType("httptransport", "\"github.com/go-kit/kit/transport/http\"\n"),
		parser.NewNameType("", "\""+endpointsImport+"\""),
	}

	handlerFile.Constants = []parser.NamedTypeValue{
		parser.NewNameTypeValue("Host", "string", `"http://10.72.17.30"`),
	}

	handlerFile.Methods = append(handlerFile.Methods, parser.NewMethodWithComment(
		"NewJWTToken",
		`NewJWTToken accept a user id as input parameter, return a string of jwt token`,
		parser.NamedTypeValue{},
		`return jwttoken.NewJWTToken(uid)`,
		[]parser.NamedTypeValue{
			parser.NewNameType("uid", "string"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "string"),
		},
	))

	handlerFile.Methods = append(handlerFile.Methods, parser.NewMethodWithComment(
		"HTTPPostJSON",
		`HTTPPostJSON submit content of JSON format string by HTTP POST`,
		parser.NamedTypeValue{},
		`
			reqUrl := host + path
			fmt.Println("URL: ", reqUrl)
			var (
				req  *http.Request
				resp *http.Response
				err  error
			)
			req, err = http.NewRequest("POST", reqUrl, bytes.NewBuffer(content))
			if err != nil {
				fmt.Println("err", err.Error())
				os.Exit(0)
			}
			req.Header.Set("Content-Type", "text/plain")
			if jwt != "" {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", jwt))
			}
			client := &http.Client{}
			resp, err = client.Do(req)
			if err != nil {
				fmt.Println("err", err.Error())
				os.Exit(0)
			}
			fmt.Println("Request Header: ", req.Header)
		
			defer resp.Body.Close()
		
			fmt.Println("response Status:", resp.Status)
			body, _ := ioutil.ReadAll(resp.Body)
			return body`,
		[]parser.NamedTypeValue{
			parser.NewNameType("host", "string"),
			parser.NewNameType("path", "string"),
			parser.NewNameType("content", "[]byte"),
			parser.NewNameType("jwt", "string"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "[]byte"),
		},
	))

	handlerFile.Methods = append(handlerFile.Methods, parser.NewMethodWithComment(
		"HTTPSPostJSON",
		`HTTPSPostJSON submit content of JSON format string by HTTPS POST`,
		parser.NamedTypeValue{},
		`
			reqUrl := host + path
			fmt.Println("URL: ", reqUrl)
			var (
				req  *http.Request
				resp *http.Response
				err  error
			)
			req, err = http.NewRequest("POST", reqUrl, bytes.NewBuffer(content))
			if err != nil {
				fmt.Println("err", err.Error())
				os.Exit(0)
			}
			req.Header.Set("Content-Type", "text/plain")
			if jwt != "" {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", jwt))
			}
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr, Timeout: time.Second * 5}
			resp, err = client.Do(req)
			if err != nil {
				fmt.Println("err", err.Error())
				os.Exit(0)
			}
			fmt.Println("Request Header: ", req.Header)
		
			defer resp.Body.Close()
		
			fmt.Println("response Status:", resp.Status)
			body, _ := ioutil.ReadAll(resp.Body)
			return body`,
		[]parser.NamedTypeValue{
			parser.NewNameType("host", "string"),
			parser.NewNameType("path", "string"),
			parser.NewNameType("content", "[]byte"),
			parser.NewNameType("jwt", "string"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "[]byte"),
		},
	))

	handlerFile.Methods = append(handlerFile.Methods, parser.NewMethodWithComment(
		"HTTPPostForm",
		`HTTPPostForm submit content of FORM format string by HTTPS POST`,
		parser.NamedTypeValue{},
		`
			reqUrl := host + path
			resp, err := http.PostForm(reqUrl, form)
			if err != nil {
				fmt.Println("http do request err ", err.Error())
				return nil
			}
			defer resp.Body.Close()
			fmt.Println("response Status:", resp.Status)
			body, _ := ioutil.ReadAll(resp.Body)
			return body`,
		[]parser.NamedTypeValue{
			parser.NewNameType("host", "string"),
			parser.NewNameType("path", "string"),
			parser.NewNameType("form", "url.Values"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "[]byte"),
		},
	))

	for _, m := range iface.Methods {
		jsonContent := "`{"
		for _,v := range m.Parameters {
			if v.Name == "ctx" {
				continue
			}
			jsonContent += fmt.Sprintf(`"%v":xxxxx`, utils.ToLowerSnakeCase(v.Name))
		}
		jsonContent = strings.Trim(jsonContent, ",")
		jsonContent = strings.TrimSpace(jsonContent)
		jsonContent += "}`"
		handlerFile.Methods = append(handlerFile.Methods,
			parser.NewMethod(
				fmt.Sprintf("Test%v", m.Name),
				parser.NamedTypeValue{},
				fmt.Sprintf(`start := time.Now()
					path := "/app/user/%s"
					content := []byte(%s)
					r := HTTPPostJSON(Host, path, content, "")
					fmt.Println("response Body:", string(r))
					fmt.Println(time.Now().Sub(start))`, utils.ToLowerHyphenCase(m.Name), jsonContent),
				[]parser.NamedTypeValue{
					parser.NewNameType("t", "*testing.T"),
				},
				[]parser.NamedTypeValue{},
			),
		)
	}

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
	fname, err := te.ExecuteString(viper.GetString("httptransport.test_file_name"), map[string]string{
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

	type ProtobufModel struct {
		Name    string
		Methods []parser.Method
	}
	pbModel := ProtobufModel{Name: utils.ToUpperFirstCamelCase(name)}
	for _, v := range iface.Methods {
		m := parser.Method{Name: v.Name}
		for k, kv := range v.Parameters {
			if kv.Type == "context.Context" {
				continue
			} else if kv.Type == "int" {
				kv.Type = "int32"
			}
			//利用 Method.Value 来传递 protobuf index，下标从 1 开始，由于 ctx 参数不用，则跨过 0 下标
			kv.Value = fmt.Sprintf("%v", k)
			kv.Name = utils.ToUpperFirstCamelCase(kv.Name)
			m.Parameters = append(m.Parameters, kv)
		}
		for k, kv := range v.Results {
			if kv.Type == "error" {
				kv.Type = "string"
			} else if kv.Type == "int" {
				kv.Type = "int32"
			}
			//利用 Method.Value 来传递 protobuf index，下标从 1 开始
			kv.Value = fmt.Sprintf("%v", k+1)
			kv.Name = utils.ToUpperFirstCamelCase(kv.Name)
			m.Results = append(m.Results, kv)
		}
		pbModel.Methods = append(pbModel.Methods, m)
	}

	protoTmpl, err := te.Execute("proto.pb", pbModel)
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
		parser.NewNameType("", `"github.com/sony/gobreaker"`),
		parser.NewNameType("stdzipkin", `"github.com/openzipkin/zipkin-go"`),
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
				parser.NewMethod("Failed", parser.NamedTypeValue{}, "", []parser.NamedTypeValue{}, []parser.NamedTypeValue{parser.NewNameType("", "error")}),
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
				parser.NewNameType("otTracer", "stdopentracing.Tracer"),
				parser.NewNameType("zipkinTracer", "*stdzipkin.Tracer"),
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

		var (
			reqPramsList string
			resPramsList string
		)
		for _, v := range reqPrams {
			reqPramsList += fmt.Sprintf("%v:%v,", v.Name, utils.ToLowerFirstCamelCase(v.Name))
		}
		for _, v := range resultPrams {
			resPramsList += fmt.Sprintf("response.%v,", v.Name)
		}
		resPramsList = strings.TrimRight(resPramsList, ",")
		//add interface method for set of endpoints
		file.Methods = append(file.Methods, parser.NewMethod(
			v.Name,
			parser.NewNameType("s", "Set"),
			fmt.Sprintf(`
			request := %sReq{%s}
			resp, err := s.%sEndpoint(ctx, request)
			if err != nil {
				return nil, err
			}
			response := resp.(%sRes)
			return %s 
			`, v.Name, reqPramsList, utils.ToUpperFirstCamelCase(v.Name), utils.ToUpperFirstCamelCase(v.Name), resPramsList),
			v.Parameters,
			v.Results,
		))

		lowerName := utils.ToLowerFirstCamelCase(v.Name)
		upperName := utils.ToUpperFirstCamelCase(v.Name)

		file.Methods[0].Body += fmt.Sprintf(`
		var %sEndpoint endpoint.Endpoint
		{
			method := "%s"
			%sEndpoint = Make%sEndpoint(svc)
            %sEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 1))(%sEndpoint)
			%sEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{}))(%sEndpoint)
			%sEndpoint = opentracing.TraceServer(otTracer, method)(%sEndpoint)
			%sEndpoint = zipkin.TraceEndpoint(zipkinTracer,  method)(%sEndpoint)
			%sEndpoint = LoggingMiddleware(log.With(logger, "method", method))(%sEndpoint)
			%sEndpoint = InstrumentingMiddleware(duration.With("method", method))(%sEndpoint)
			%sEndpoint = jwt.NewParser(kf, stdjwt.SigningMethodHS256, claimsFactory)(%sEndpoint)
			set.%sEndpoint = %sEndpoint
		}
		`, lowerName,
			lowerName,
			lowerName,
			upperName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			lowerName,
			upperName,
			lowerName)
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
		parser.NewNameType("", `"github.com/go-kit/kit/metrics"`),
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
		parser.NewNameType("", `"github.com/go-kit/kit/metrics"`),
		parser.NewNameType("", `"github.com/go-kit/kit/log"`),
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
				requestCount:  requestCount,
				requestLatency: requestLatency,
				next:  next,
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
