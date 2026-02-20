package main
import (
 "fmt"
 "github.com/IBM/sarama"
)
func main(){
 cfg:=sarama.NewConfig()
 cfg.Version = sarama.V2_6_0_0
 client,err:=sarama.NewClient([]string{"127.0.0.1:19092"}, cfg)
 if err!=nil { panic(err) }
 defer client.Close()
 topics,err:=client.Topics()
 if err!=nil { panic(err) }
 fmt.Println(topics)
}
