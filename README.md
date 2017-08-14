# Go-Kit 生成器
Go-kit 生成器是一个命令行应用程序，可根据参数生成go-kit service模板代码
## Why?

```
**Because I'm lazy**, and because it would make it easier for go-kit newcomers to start using it.
```
上面是原作者开发这个程序的原因，对于我：
* 这样一个可方便生成go-kit代码的程序，不但可以减少手敲代码的数量，还可以预防很多错误
* 原项目在我fork的时候已经很长时间没有更新功能了，由于go-kit addsvc调整了代码布局，我急需一个可以生成与go-kit官方代码布局相同的生成器
## 安装
```bash
go get github.com/yiv/gk
go install github.com/yiv/gk
```
## 运行

`gk`必须要在`$GOPATH`目录及其任何子目录下才能运行，不一定要`$GOPATH`根目录，你可以在`$GOPATH`路径下创建一个工程目录，然后在该目录运行。

`gk`首次运行的时候会在运行目录查找`gk.json`配置文件，如果未找到，它会根据默认配置生成新的`gk.json`文件

#### 创建新 service
在工程目录下运行:
```bash
gk new service hello
```
或使用短命令:
```bash
gk n s hello
```
运行本命令会生成一个 `helloservice` :
```
project/
├── gk.json
└── hello
    └── pkg
        └── helloservice
            └── service.go

```
**service.go**
```go
package service
// Implement yor service methods methods.
// e.x: Foo(ctx context.Context,s string)(rs string,err error)
type HelloService interface {
}
```
Now you need to add the interface methods and initiate your service:
e.x:
```go
package helloservice

// Implement yor service methods methods.
// e.x: Foo(ctx context.Context,bar string)(rs string, err error)
type Service interface {
}
```
然后运行 : 
```bash
gk init hello
```
运行这个命令后会新增如下代码文件 
```
.
├── gk.json
└── hello
    └── pkg
        ├── helloendpoint
        │   ├── middleware.go
        │   └── set.go
        ├── helloservice
        │   ├── instrumenting.go
        │   ├── logging.go
        │   └── service.go
        └── hellotransport
            └── http.go
```


The final folder structure is the same as  [addsvc](https://github.com/peterbourgon/go-microservices/tree/master/addsvc) 
By Default the generator will use `default_transport` setting from `gk.json` and create the transport. If you want to specify
the transport use `-t` flag
```bash
gk init hello -t grpc
```

## 添加其它 transports
在现在工程里添加其它 transports 运行 `gk add [transporteType] [serviceName]`   
e.x adding grpc:
```bash
gk add grpc hello
```
运行上面的命令后，你会在控制台看到如下打印信息：  
```bash
INFO[0000] Generating grpc transport...                 
WARN[0000] -------------------------------------------------------------------- 
WARN[0000] The service is still not ready!!             
WARN[0000] To create the grpc transport please create your protobuf. 
WARN[0000] Than follow the instructions in compile.sh and compile the .proto file. 
WARN[0000] After the file is compiled run `gk init grpc hello`. 
WARN[0000] -------------------------------------------------------------------- 
```
此时的代码目录结构是这样的：
```
.
├── gk.json
└── hello
    ├── pb
    │   ├── compile.bat
    │   └── hello.proto
    └── pkg
        ├── helloendpoint
        │   ├── middleware.go
        │   └── set.go
        ├── helloservice
        │   ├── instrumenting.go
        │   ├── logging.go
        │   └── service.go
        └── hellotransport
            └── http.go
```
完成 grpc transport的添加，你需要先编译 protobuffer 文件，然后再运行下面的命令
```bash
gk init grpc hello
```
下面是完成后的目录结果
```
.
├── gk.json
└── hello
    ├── pb
    │   ├── compile.bat
    │   ├── hello.pb.go
    │   └── hello.proto
    └── pkg
        ├── helloendpoint
        │   ├── middleware.go
        │   └── set.go
        ├── helloservice
        │   ├── instrumenting.go
        │   ├── logging.go
        │   └── service.go
        └── hellotransport
            └── http.go
```
下面是原项目的代码布局：
```
project/
└── pkg
    ├── endpoints
    │   └── endpoints.go
    ├── grpc
    │   ├── handler.go
    │   └── pb
    │       ├── compile.bat
    │       ├── hello.pb.go
    │       └── hello.proto
    ├── http
    │   └── handler.go
    └── service
        └── service.go
```

## I don't like the folder structure!

The folder structure that the generator is using is following https://github.com/go-kit/kit/issues/70 but 
that can be changed using `gk.json` all the paths are configurable there.

## Cli Help
Every command has the `-h` or `--help` flag this will give you more info on what the command does and how to use it.
e.x 
```bash
gk init -h
```
will return
```bash
Initiates a service

Usage:
  gk init [flags]

Flags:
  -t, --transport string   Specify the transport you want to initiate for the service

Global Flags:
  -d, --debug           If you want to se the debug logs.
      --folder string   If you want to specify the base folder of the project.
  -f, --force           Force overide existing files without asking.
      --testing         If testing the generator.

```
## What is working
The example you see here  https://github.com/go-kit/kit/issues/70

## Examples
You can find examples under the `test_dir`

## TODO-s

 - Implement the update commands, this commands would be used to update an existing service e.x add 
 a new request parameter to an endpoint(Probably not needed).
 - Implement middleware generator (service,endpoint).
 - Implement automatic creation of the service main file.
 - Tests tests tests ...
## Warnings

- I only tested this on the mac, should work on other os-s but I have not tested it, I would appreciate feedback on this. 
## Contribute
Thanks a lot for contributing. 

To test your new features/bug-fixes you need a way to run `gk` inside your project this can be done using `test_dir`.

Execute this in your command line :
```bash
export GK_FOLDER="test_dir" 
```
Create a folder in the `gk` repository called `test_dir`, now every time you run `go run main.go [anything]`
`gk` will treat `test_dir` as the project root.

If you edit the templates you need to run `compile.sh` inside the templates folder.
 
