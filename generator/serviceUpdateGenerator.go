package generator

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	template "github.com/yiv/gk/templates"
	"golang.org/x/tools/imports"

	"github.com/yiv/gk/fs"
	"github.com/yiv/gk/parser"
	"github.com/yiv/gk/utils"
)

type ServiceUpdateGenerator struct {
}

func NewServiceUpdateGenerator() *ServiceUpdateGenerator {
	return &ServiceUpdateGenerator{}
}

func (sg *ServiceUpdateGenerator) Generate(name string) error {
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
		if v.Name == "NewBasicService" {
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

func (sg *ServiceUpdateGenerator) generateServiceLoggingMiddleware(name string, iface *parser.Interface) error {
	logrus.Info("Updating service logging middleware...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile := path + defaultFs.FilePathSeparator() + "logging.go"

	s, err := defaultFs.ReadFile(sfile)
	if err != nil {
		return err
	}

	p := parser.NewFileParser()
	file, err := p.Parse([]byte(s))
	if err != nil {
		return err
	}

	for _, v := range iface.Methods {
		var isExist bool
		for _, vv := range file.Methods {
			if v.Name == vv.Name {
				isExist = true
				break
			}
		}
		if isExist {
			continue
		}
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

func (sg *ServiceUpdateGenerator) generateServiceInstrumentingMiddleware(name string, iface *parser.Interface) error {
	logrus.Info("Updating service instrumenting middleware...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile := path + defaultFs.FilePathSeparator() + "instrumenting.go"

	s, err := defaultFs.ReadFile(sfile)
	if err != nil {
		return err
	}

	p := parser.NewFileParser()
	file, err := p.Parse([]byte(s))
	if err != nil {
		return err
	}

	for _, v := range iface.Methods {
		var isExist bool
		for _, vv := range file.Methods {
			if v.Name == vv.Name {
				isExist = true
				break
			}
		}
		if isExist {
			continue
		}
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

func (sg *ServiceUpdateGenerator) generateEndpoints(name string, iface *parser.Interface) error {
	logrus.Info("Updating endpoints...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	enpointsPath, err := te.ExecuteString(viper.GetString("endpoints.path"), map[string]string{
		"ServiceName": name,
	})
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

	s, err := defaultFs.ReadFile(eFile)
	if err != nil {
		return err
	}

	p := parser.NewFileParser()
	file, err := p.Parse([]byte(s))
	if err != nil {
		return err
	}

	getSetStruct := func() *parser.Struct {
		for k, v := range file.Structs {
			if v.Name == "Set" {
				return &file.Structs[k]
			}
		}
		return nil
	}
	var setStruct *parser.Struct
	if setStruct = getSetStruct(); setStruct == nil {
		return errors.New("set struct not found")
	}

	getNewMethodIndex := func() int {
		for k, v := range file.Methods {
			if v.Name == "New" {
				return k
			}
		}
		return 0
	}
	newMethodIndex := getNewMethodIndex()

	file.Methods[newMethodIndex].Body = strings.ReplaceAll(file.Methods[newMethodIndex].Body, "return set", "")

	for _, v := range iface.Methods {
		var isExist bool
		for _, vv := range file.Methods {
			if vv.Name == v.Name {
				isExist = true
			}
		}
		if isExist {
			continue
		}
		setStruct.Vars = append(setStruct.Vars, parser.NewNameType(v.Name+"Endpoint", "endpoint.Endpoint"))
	}

	for _, v := range iface.Methods {
		reqStructName := v.Name + "Req"
		var isExist bool
		for _, vv := range file.Structs {
			if vv.Name == reqStructName {
				isExist = true
			}
		}
		if isExist {
			continue
		}
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
				parser.NewNameType("err", "error"),
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
			request := %sReq{%s}
			resp, err := s.%sEndpoint(ctx, request)
			if err != nil {
				return %v
			}
			response := resp.(%sRes)
			return %s 
			`, v.Name,
				ToReqList(reqPrams),
				utils.ToUpperFirstCamelCase(v.Name),
				ToErrResList(resultPrams),
				utils.ToUpperFirstCamelCase(v.Name),
				ToResList(resultPrams)),
			v.Parameters,
			v.Results,
		))

		lowerName := utils.ToLowerFirstCamelCase(v.Name)
		upperName := utils.ToUpperFirstCamelCase(v.Name)

		file.Methods[newMethodIndex].Body += fmt.Sprintf(`
			var %sEndpoint endpoint.Endpoint
			{
				method := "%s"
				%sEndpoint = Make%sEndpoint(svc)
	            %sEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 10000))(%sEndpoint)
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

	file.Methods[newMethodIndex].Body += "\n return set"

	err = defaultFs.WriteFile(eFile, file.String(), false)
	if err != nil {
		return err
	}

	return nil
}

func (sg *ServiceUpdateGenerator) generateTransport(name string, iface *parser.Interface, transport string) error {
	switch transport {
	case "http":
		if err := sg.generateHttpTransport(name, iface); err != nil {
			return err
		}
		if err := sg.generateHttpTransportTesting(name, iface); err != nil {
			return err
		}
		return nil
	default:
		return errors.New(fmt.Sprintf("Transport `%s` not supported", transport))
	}
}

func (sg *ServiceUpdateGenerator) generateHttpTransport(name string, iface *parser.Interface) error {
	logrus.Info("Updating http transport...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("httptransport.path"), map[string]string{
		"ServiceName":   name,
		"TransportType": "http",
	})
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

	s, err := defaultFs.ReadFile(tfile)
	if err != nil {
		return err
	}

	p := parser.NewFileParser()
	handlerFile, err := p.Parse([]byte(s))
	if err != nil {
		return err
	}

	handlerFile.Methods[0].Body = strings.ReplaceAll(handlerFile.Methods[0].Body, "return m", "")

	for _, m := range iface.Methods {
		decodeReqFunName := fmt.Sprintf("decodeHTTP%sReq", m.Name)
		var isExist bool
		for _, v := range handlerFile.Methods {
			if v.Name == decodeReqFunName {
				isExist = true
			}
		}
		if isExist {
			continue
		}
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

	return defaultFs.WriteFile(tfile, handlerFile.String(), false)
}

func (sg *ServiceUpdateGenerator) generateHttpTransportTesting(name string, iface *parser.Interface) error {
	logrus.Info("Updating http transport...")
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("httptransport.path"), map[string]string{
		"ServiceName":   name,
		"TransportType": "http",
	})
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

	s, err := defaultFs.ReadFile(tfile)
	if err != nil {
		return err
	}

	p := parser.NewFileParser()
	handlerFile, err := p.Parse([]byte(s))
	if err != nil {
		return err
	}

	for _, m := range iface.Methods {
		testFunName := fmt.Sprintf("Test%v", m.Name)
		var isExist bool
		for _, v := range handlerFile.Methods {
			if v.Name == testFunName {
				isExist = true
			}
		}
		if isExist {
			continue
		}

		jsonContent := "`{"
		for _, v := range m.Parameters {
			if v.Name == "ctx" {
				continue
			}
			jsonContent += fmt.Sprintf(`"%v":xxxxx,`, utils.ToLowerSnakeCase(v.Name))
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
					r := HTTPPostJSON(Host, path, content, NewJWTToken(Uid))
					fmt.Println("response Body:", string(r))
					fmt.Println(time.Now().Sub(start))`, utils.ToLowerHyphenCase(m.Name), jsonContent),
				[]parser.NamedTypeValue{
					parser.NewNameType("t", "*testing.T"),
				},
				[]parser.NamedTypeValue{},
			),
		)
	}

	return defaultFs.WriteFile(tfile, handlerFile.String(), false)
}

func (sg *ServiceUpdateGenerator) generateGRPCTransport(name string, iface *parser.Interface) error {
	logrus.Info("Updating grpc transport...")
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
		Structs []parser.Struct
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
			kv.Type = strings.ReplaceAll(kv.Type, "[]", "repeated ")
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

			if strings.Contains(kv.Type, "map") {
				//map[string]string
				tmp := strings.Split(kv.Type, "[")
				tmp = strings.Split(tmp[1], "]")
				mapKeyType := tmp[0]
				mapValueType := tmp[1]
				if strings.Contains(mapValueType, ".") {
					tmp = strings.Split(mapValueType, ".")
					mapValueType = tmp[1]
					pbModel.Structs = append(pbModel.Structs, parser.NewStruct(mapValueType, nil))
				}
				kv.Type = fmt.Sprintf("map<%v,%v> ", mapKeyType, mapValueType)
			} else if strings.Contains(kv.Type, "[]") {
				var elementType string
				if strings.Contains(kv.Type, ".") {
					tmp := strings.Split(kv.Type, ".")
					elementType = tmp[1]
					pbModel.Structs = append(pbModel.Structs, parser.NewStruct(elementType, nil))
					kv.Type = fmt.Sprintf("repeated %s ", elementType)
				} else {
					kv.Type = strings.ReplaceAll(kv.Type, "[]", "repeated ")
				}
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
