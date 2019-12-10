package parser

import (
	"fmt"
	"log"
	"testing"
)

func TestProtoParse(t *testing.T) {
	pp := NewProtoParser()
	file := `package pb;

	import "github.com/gogo/protobuf/gogoproto/gogo.proto";
	
	option (gogoproto.goproto_unkeyed_all) = xx;
	option (gogoproto.goproto_unrecognized_all) = false;
	option (gogoproto.goproto_sizecache_all) = false;
	
	service User {
		rpc SaveBank (EmptyReq) returns (EmptyRes) {}
	}
	
	message EmptyReq {
		repeated int64 Uid = 1 [(gogoproto.jsontag) = "uid", (gogoproto.moretags) = 'xorm:"uid"'];
	}
	message EmptyRes {
		string Err = 1;
		map<int64,double> Coin = 2;
	}`
	p, err := pp.Parse([]byte(file))
	if err != nil {
		log.Fatal("err", err.Error())
	}

	fmt.Printf("PackageName %#v \n", p.PackageName)
	fmt.Printf("ServiceName %#v \n", p.ServiceName)
	fmt.Printf("Methods %#v \n", p.Methods)
	fmt.Printf("Message %#v \n", p.Messages)
	fmt.Printf("Imports %#v \n", p.Imports)
	fmt.Printf("Options %#v \n", p.Options)

}
