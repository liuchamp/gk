package generator

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/liuchamp/gk/fs"
	"github.com/liuchamp/gk/parser"
	template "github.com/liuchamp/gk/templates"
	"github.com/liuchamp/gk/utils"
)

type AddGRPCGenerator struct {
}

func NewAddGRPCGenerator() *AddGRPCGenerator {
	return &AddGRPCGenerator{}
}

func (sg *AddGRPCGenerator) ParseService(name string) (*parser.Interface, error) {
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return nil, err
	}
	fname, err := te.ExecuteString(viper.GetString("service.file_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return nil, err
	}
	sfile := path + defaultFs.FilePathSeparator() + fname
	b, err := defaultFs.Exists(sfile)
	if err != nil {
		return nil, err
	}
	iname, err := te.ExecuteString(viper.GetString("service.interface_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return nil, err
	}
	if !b {
		return nil, errors.New(fmt.Sprintf("Service %s was not found", name))
	}
	p := parser.NewFileParser()
	s, err := defaultFs.ReadFile(sfile)
	if err != nil {
		return nil, err
	}
	f, err := p.Parse([]byte(s))
	if err != nil {
		return nil, err
	}
	var iface *parser.Interface
	for _, v := range f.Interfaces {
		if v.Name == iname {
			iface = &v
		}
	}
	if iface == nil {
		return nil, errors.New(fmt.Sprintf("Could not find the service interface in `%s`", sfile))
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
		return nil, errors.New("The service has no method please implement the interface methods")
	}
	return iface, nil
}

func (sg *AddGRPCGenerator) GenerateProtobuf(name string) (err error) {
	var iface *parser.Interface
	iface, err = sg.ParseService(name)
	if err != nil {
		return err
	}
	logrus.Info("Generating grpc transport...")
	te := template.NewEngine()
	defaultFs := fs.Get()

	path, err := te.ExecuteString(viper.GetString("pb.path"), map[string]string{
		"ServiceName": name,
		//"TransportType": "grpc",
	})

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
			err = sg.UpdateProtobuf(name, iface, tfile, defaultFs, te)
			return err
		}
	} else {
		err = defaultFs.MkdirAll(path)
		if err != nil {
			return err
		}
	}

	pbModel := &parser.Proto{PackageName: fmt.Sprintf("%vpb", name), ServiceName: utils.ToUpperFirstCamelCase(name)}
	pbModel = TransferToPBModel(pbModel, iface)

	protoTmpl, err := te.Execute("proto.pb", pbModel)
	if err != nil {
		fmt.Println("edwin #199 ", err.Error())
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
	return nil
}

func (sg *AddGRPCGenerator) UpdateProtobuf(name string, iface *parser.Interface, sfile string, defaultFs *fs.DefaultFs, te template.Engine) (err error) {
	pp := parser.NewProtoParser()
	s, err := defaultFs.ReadFile(sfile)
	if err != nil {
		logrus.Error("err", err.Error())
		return err
	}
	var pbModel *parser.Proto
	if pbModel, err = pp.Parse([]byte(s)); err != nil {
		logrus.Error("err", err.Error())
		fmt.Println("=========s==========")
		fmt.Printf("%v\n", string(s))
		fmt.Println("=========e==========")
		return err
	}

	pbModel = TransferToPBModel(pbModel, iface)

	protoTmpl, err := te.Execute("proto.pb", pbModel)
	if err != nil {
		fmt.Println("edwin #199 ", err.Error())
		return err
	}

	err = defaultFs.WriteFile(sfile, protoTmpl, false)

	return nil
}

func TransferToPBModel(pbModel *parser.Proto, iface *parser.Interface) *parser.Proto {
	for _, v := range iface.Methods {
		var isExist bool
		for _, vv := range pbModel.Methods {
			if vv.Name == v.Name {
				isExist = true
				break
			}
		}
		if isExist {
			continue
		}
		var (
			msgReq = parser.Struct{Name: fmt.Sprintf("%vReq", utils.ToUpperFirstCamelCase(v.Name))}
			msgRes = parser.Struct{Name: fmt.Sprintf("%vRes", utils.ToUpperFirstCamelCase(v.Name))}
		)
		m := parser.Method{Name: v.Name}
		for k, kv := range v.Parameters {
			if kv.Type == "context.Context" {
				continue
			}

			var otherMessages []parser.Struct
			kv.Type, otherMessages = ParseToPBType(kv.Type)
			if len(otherMessages) > 0 {
				for _, ov := range otherMessages {
					if isExist := IsMessageExist(pbModel.Messages, ov); isExist {
						continue
					} else {
						pbModel.Messages = append(pbModel.Messages, parser.NewStruct(ov.Name, nil))
					}
				}
			}

			//利用 Method.Value 来传递 protobuf index，下标从 1 开始，由于 ctx 参数不用，则跨过 0 下标
			kv.Value = fmt.Sprintf("%v", k)
			kv.Name = utils.ToUpperFirstCamelCase(kv.Name)
			msgReq.Vars = append(msgReq.Vars, kv)
		}
		for k, kv := range v.Results {
			var otherMessages []parser.Struct
			kv.Type, otherMessages = ParseToPBType(kv.Type)
			if len(otherMessages) > 0 {
				for _, ov := range otherMessages {
					if isExist := IsMessageExist(pbModel.Messages, ov); isExist {
						continue
					} else {
						pbModel.Messages = append(pbModel.Messages, parser.NewStruct(ov.Name, nil))
					}
				}
			}
			//利用 Method.Value 来传递 protobuf index，下标从 1 开始
			kv.Value = fmt.Sprintf("%v", k+1)
			kv.Name = utils.ToUpperFirstCamelCase(kv.Name)
			msgRes.Vars = append(msgRes.Vars, kv)
		}
		pbModel.Methods = append(pbModel.Methods, m)
		pbModel.Messages = append(pbModel.Messages, msgReq, msgRes)
	}
	return pbModel
}
func IsMessageExist(messages []parser.Struct, msg parser.Struct) (yes bool) {
	for _, mv := range messages {
		if mv.Name == msg.Name {
			yes = true
			break
		}
	}
	return
}

func ParseToPBType(dataType string) (pbDataType string, otherMessages []parser.Struct) {
	if dataType == "error" {
		pbDataType = "string"
		return
	} else if dataType == "int" {
		pbDataType = "int32"
		return
	}

	var (
		mapKeyType string
		isRepeated bool
	)
	if strings.Contains(dataType, "map") {
		//map[string]string
		tmp := strings.Split(dataType, "[")
		tmp = strings.Split(tmp[1], "]")
		mapKeyType = tmp[0]
		dataType = tmp[1]
	}

	if strings.Contains(dataType, "[]") {
		isRepeated = true
		dataType = strings.ReplaceAll(dataType, "[]", "")
	}
	if strings.Contains(dataType, ".") {
		tmp := strings.Split(dataType, ".")
		dataType = tmp[1]
		otherMessages = append(otherMessages, parser.NewStruct(dataType, nil))
	}

	if mapKeyType != "" && isRepeated {
		panic(fmt.Errorf("proto does not support map with array value"))
	}

	if mapKeyType != "" {
		pbDataType = fmt.Sprintf("map<%v,%v> ", mapKeyType, dataType)
		return
	} else if isRepeated {
		pbDataType = fmt.Sprintf("repeated %s ", dataType)
		return
	} else {
		pbDataType = dataType
	}
	return
}
