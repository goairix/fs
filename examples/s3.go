package examples

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	f "github.com/goairix/fs"
	"github.com/goairix/fs/driver/s3"
)

func S3() {
	cnf := s3.Config{
		Region:          "us-east-1",
		Endpoint:        "https://s3.amazonaws.com", // 可选，用于兼容其他S3协议的存储服务
		AccessKeyID:     "your-access-key",
		SecretAccessKey: "your-secret-key",
		BucketName:      "your-bucket",
	}

	// 创建文件系统实例
	fs, err := s3.New(cnf)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 创建目录
	err = fs.MakeDir(ctx, "test", 0755)
	if err != nil {
		log.Fatal(err)
	}

	// 创建并写入文件
	writer, err := fs.Create(
		ctx,
		"test/hello.txt",
		f.WithContentType("text/plain"),
		f.WithMetadata(map[string]interface{}{
			"Author": "dysodeng",
			"Time":   time.Now().Format(time.DateTime),
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	content := []byte("Hello, S3 File System!")
	_, err = writer.Write(content)
	if err != nil {
		_ = writer.Close()
		log.Fatal("文件写入错误：" + err.Error())
	}
	_ = writer.Close()

	// 读取文件
	reader, err := fs.Open(ctx, "test/hello.txt")
	if err != nil {
		log.Fatal("读取文件错误：" + err.Error())
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		log.Fatal("读取文件内容错误：" + err.Error())
	}
	fmt.Printf("文件内容: %s\n", string(data))

	// 复制文件
	err = fs.Copy(ctx, "test/hello.txt", "test/hello_copy.txt")
	if err != nil {
		log.Fatal("复制文件错误：" + err.Error())
	}

	// 复制目录
	err = fs.CopyDir(ctx, "test", "test_copy")
	if err != nil {
		log.Fatal("复制目录错误：" + err.Error())
	}

	// 移动目录
	err = fs.MoveDir(ctx, "test_copy", "test_moved")
	if err != nil {
		log.Fatal("移动目录错误：" + err.Error())
	}

	// 重命名目录
	err = fs.RenameDir(ctx, "test_moved", "test_renamed")
	if err != nil {
		log.Fatal("重命名目录错误：" + err.Error())
	}

	// 列出目录内容
	files, err := fs.List(ctx, "test")
	if err != nil {
		log.Fatal("列出目录错误：" + err.Error())
	}
	fmt.Println("目录内容:")
	for _, file := range files {
		fmt.Printf("- %s\n", file.Name())
	}

	// 文件/目录是否存在
	exists, err := fs.Exists(ctx, "test")
	if err != nil {
		log.Fatal("判断文件/目录是否存在错误：" + err.Error())
	}
	fmt.Printf("文件/目录是否存在：%t\n", exists)

	// 文件信息
	info, err := fs.Stat(ctx, "test/hello.txt")
	if err != nil {
		log.Fatal("获取文件信息错误：" + err.Error())
	}
	fmt.Println("文件信息:")
	fmt.Printf("--->文件名: %s\n", info.Name())
	fmt.Printf("--->文件大小: %d\n", info.Size())
	fmt.Printf("--->文件权限: %s\n", info.Mode())
	fmt.Printf("--->文件修改时间: %s\n", info.ModTime().Format(time.DateTime))
	mimeType, err := fs.GetMimeType(ctx, "test/hello.txt")
	if err != nil {
		log.Fatal("获取文件MIME类型错误：" + err.Error())
	}
	fmt.Printf("--->文件MimeType: %s\n", mimeType)

	// 获取文件元数据
	metadata, err := fs.GetMetadata(ctx, "test/hello.txt")
	if err != nil {
		log.Fatal("读取文件元数据错误：" + err.Error())
	}
	fmt.Printf("文件元数据: %+v\n", metadata)

	// 删除文件
	err = fs.Remove(ctx, "test/hello_copy.txt")
	if err != nil {
		log.Fatal(err)
	}
}
