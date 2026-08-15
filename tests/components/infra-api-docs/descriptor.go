package main

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// methodNamesOf 从反射返回的 FileDescriptorProto 里取出某个服务的方法名。
//
// 用真正的描述符解析，而不是在字节里找字符串：后者看起来能跑，
// 但一个方法名恰好出现在别的字段里就会多列一个不存在的方法——
// 而文档里多出一个调不通的方法，比少一个更误导人。
func methodNamesOf(raw []byte, service string) []string {
	var file descriptorpb.FileDescriptorProto
	if err := proto.Unmarshal(raw, &file); err != nil {
		return nil
	}

	for _, svc := range file.GetService() {
		full := svc.GetName()
		if pkg := file.GetPackage(); pkg != "" {
			full = pkg + "." + full
		}
		if full != service {
			continue
		}

		methods := make([]string, 0, len(svc.GetMethod()))
		for _, method := range svc.GetMethod() {
			methods = append(methods, method.GetName())
		}
		return methods
	}
	return nil
}
