package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/gogo/protobuf/proto"
	descriptor "github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

type TSDefGenerator struct {
	Request    *plugin.CodeGeneratorRequest
	Response   *plugin.CodeGeneratorResponse
	Parameters map[string]string
}

type MessageLocation struct {
	Location *descriptor.SourceCodeInfo_Location
	Message  *descriptor.DescriptorProto
	Comments []string
}

func (runner *TSDefGenerator) PrintParameters(w io.Writer) {
	const padding = 3
	tw := tabwriter.NewWriter(w, 0, 0, padding, ' ', tabwriter.TabIndent)
	fmt.Fprintf(tw, "Parameters:\n")
	for k, v := range runner.Parameters {
		fmt.Fprintf(tw, "%s:\t%s\n", k, v)
	}
	fmt.Fprintln(tw, "")
	tw.Flush()
}

func (runner *TSDefGenerator) getMessageLocation() map[string][]*MessageLocation {

	ret := make(map[string][]*MessageLocation)
	for index, filename := range runner.Request.FileToGenerate {
		locationMessages := make([]*MessageLocation, 0)
		proto := runner.Request.ProtoFile[index]
		desc := proto.GetSourceCodeInfo()
		locations := desc.GetLocation()
		for _, location := range locations {
			// I would encourage developers to read the documentation about paths as I might have misunderstood this
			// I am trying to process message types which I understand to be `4` and only at the root level which I understand
			// to be path len == 2
			if len(location.GetPath()) > 2 {
				continue
			}

			leadingComments := strings.Split(location.GetLeadingComments(), "\n")
			if len(location.GetPath()) > 1 && location.GetPath()[0] == int32(4) {
				message := proto.GetMessageType()[location.GetPath()[1]]

				locationMessages = append(locationMessages, &MessageLocation{
					Message:  message,
					Location: location,
					// Because we are only parsing messages here at the root level we will not get field comments
					Comments: leadingComments[:len(leadingComments)-1],
				})
			}
		}
		ret[filename] = locationMessages
	}
	return ret
}

func typeToTypeTS(field *descriptor.FieldDescriptorProto) string {

	if field.GetLabel().String() == "LABEL_REPEATED" {
		return fmt.Sprintf("Array<%s>", getTSType(field))
	} else {
		return getTSType(field)
	}

}

func getTSType(field *descriptor.FieldDescriptorProto) string {
	os.Stderr.WriteString(fmt.Sprintf("Created File: %s \n", field.GetType().String()))
	os.Stderr.WriteString(fmt.Sprintf("Created FIELD: %s \n", field.String()))

	switch field.GetType().String() {
	case "TYPE_STRING":
		return "string"
	case "TYPE_INT32":
		return "number"
	case "TYPE_MESSAGE":
		typeName := field.GetTypeName()
		depNames := strings.Split(typeName, ".")
		return depNames[len(depNames)-1]
	default:
		return "any"
	}

}

func (runner *TSDefGenerator) createTSFile(filename string, messages []*MessageLocation) error {
	// Create a file and append it to the output files

	var outfileName string
	var content string
	outfileName = strings.Replace(filename, ".proto", ".ts", -1)
	var mdFile plugin.CodeGeneratorResponse_File
	mdFile.Name = &outfileName
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("// %s\n", outfileName))
	for _, locationMessage := range messages {
		buf.WriteString(fmt.Sprintf("\nexport interface %s { \n", locationMessage.Message.GetName()))
		buf.WriteString(fmt.Sprintf("/* %s\n", "Leading Comments"))
		for _, comment := range locationMessage.Comments {
			buf.WriteString(fmt.Sprintf("%s\n", comment))
		}
		buf.WriteString(fmt.Sprintf("*/ \n"))

		if len(locationMessage.Message.NestedType) > 0 {
			buf.WriteString(fmt.Sprintf("// %s\n", "Nested Messages"))
			for _, nestedMessage := range locationMessage.Message.NestedType {
				buf.WriteString(fmt.Sprintf("\nexport interface  %s {\n", nestedMessage.GetName()))
				buf.WriteString(fmt.Sprintf("// %s\n", "Fields"))
				for _, field := range nestedMessage.Field {
					buf.WriteString(fmt.Sprintf("    %s: %s;\n", field.GetName(), typeToTypeTS(field)))
				}

				buf.WriteString(fmt.Sprintf("}\n\n "))
			}
		}
		for _, field := range locationMessage.Message.Field {
			buf.WriteString(fmt.Sprintf("   %s: %s;\n", field.GetName(), typeToTypeTS(field)))
		}

		buf.WriteString(fmt.Sprintf("}\n"))
	}
	content = buf.String()
	mdFile.Content = &content
	runner.Response.File = append(runner.Response.File, &mdFile)
	os.Stderr.WriteString(fmt.Sprintf("Created File: %s \n", filename))
	return nil
}

func (runner *TSDefGenerator) generateMessageMarkdown() error {
	// This convenience method will return a structure of some types that I use
	fileLocationMessageMap := runner.getMessageLocation()
	for filename, locationMessages := range fileLocationMessageMap {
		runner.createTSFile(filename, locationMessages)
	}
	return nil
}

func (runner *TSDefGenerator) generateCode() error {
	// Initialize the output file slice
	files := make([]*plugin.CodeGeneratorResponse_File, 0)
	runner.Response.File = files

	{
		err := runner.generateMessageMarkdown()
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	// os.Stdin will contain data which will unmarshal into the following object:
	// https://godoc.org/github.com/golang/protobuf/protoc-gen-go/plugin#CodeGeneratorRequest
	req := &plugin.CodeGeneratorRequest{}
	resp := &plugin.CodeGeneratorResponse{}

	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}

	err = req.Unmarshal(data)
	if err != nil {
		panic(err)
	}

	parameters := req.GetParameter()

	defGenerator := &TSDefGenerator{
		Request:    req,
		Response:   resp,
		Parameters: make(map[string]string),
	}
	groupkv := strings.Split(parameters, ",")
	for _, element := range groupkv {
		kv := strings.Split(element, "=")
		if len(kv) > 1 {
			defGenerator.Parameters[kv[0]] = kv[1]
		}
	}
	// Print the parameters for example
	defGenerator.PrintParameters(os.Stderr)

	err = defGenerator.generateCode()
	if err != nil {
		panic(err)
	}

	marshalled, err := proto.Marshal(resp)
	if err != nil {
		panic(err)
	}
	os.Stdout.Write(marshalled)
}
