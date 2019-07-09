package parser

import (
	"fmt"
	"log"
	"testing"
)

func TestProtoParse(t *testing.T) {
	pp := NewProtoParser()
	file := `package pb;
	
	service User {
		rpc SaveBank (EmptyReq) returns (EmptyRes) {}
	}
	
	message EmptyReq {
		repeated int64 Uid = 1;
	}
	message EmptyRes {
		string Err = 1;
		map<int64,double> Coin = 2;
	}`
	p, err := pp.Parse([]byte(file))
	if err != nil {
		log.Fatal("err", err.Error())
	}

	//fmt.Printf("PackageName %#v \n", p.PackageName)
	fmt.Printf("ServiceName %#v \n", p.ServiceName)
	fmt.Printf("Methods %#v \n", p.Methods)
	fmt.Printf("Message %#v \n", p.Messages)

}
