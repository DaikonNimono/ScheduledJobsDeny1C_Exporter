package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"encoding/json"
	"golang.org/x/text/encoding/charmap"
	"log"
	"os/exec"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	PathToRac string `json:"path_to_rac"`
	ExporterPort string `json:"exporter_port"`
	Cluster []struct {
		Cluster  string `json:"cluster"`
		Name     string `json:"name"`
		Infobase []struct {
			Infobase          string `json:"infobase"`
			User 		      string `json:"user"`
			Name              string `json:"name"`
			Password string `json:"password"`
			ScheduledJobsDeny string `json:"scheduled-jobs-deny"`
		} `json:"infobase"`
	} `json:"cluster"`
}


func cmdExec(name string, args ...string) (out []byte, sliceOfMaps []map[string]string) {
	result, err := exec.Command(name, args...).Output()
	if err != nil {
		log.Println(err)
	}

	if len(result) == 0 {
		return nil, nil
	}
	d := charmap.CodePage866.NewDecoder()
	out, err = d.Bytes(result)
	if err != nil {
		log.Println("Ошибка CodePage866", err)
	}

	splitOut := strings.Split(string(out), "\r\n\r\n")
	if len(splitOut) > 1 {
		splitOut = splitOut[:len(splitOut)-1]
	}

	for i := range splitOut {
		splitOut[i] = strings.TrimSpace(splitOut[i])
		if splitOut[i] == "" {
			fmt.Println("Пустой элемент!!")
		}
	}

	var pString []string
	var paragraph [][]string

	for i := range splitOut {
		x := strings.Split(splitOut[i], "\r\n")
		pString = append(pString, x...)
		if i <= len(splitOut) {
			paragraph = append(paragraph, pString)
			pString = []string{}
			continue
		}
	}

	for i := range paragraph {
		for j := range paragraph[i] {
			paragraph[i][j] = strings.ReplaceAll(paragraph[i][j], ":", "~:~")
			if paragraph[i][j] == "" {
				fmt.Println("Пустой элемент!!")
			}
		}
	}

	mapVar := map[string]string{}
	sliceOfMaps = []map[string]string{}

	for j := range paragraph {
		for i := range paragraph[j] {
			x := strings.Split(paragraph[j][i], "~:~") //
			if len(x) == 1 {
				continue
			}
			x[0] = strings.TrimSpace(x[0])
			x[1] = strings.TrimSpace(x[1])
			x[1] = strings.Trim(x[1], "\"")
			mapVar[x[0]] = x[1]
		}
		sliceOfMaps = append(sliceOfMaps, mapVar)
		mapVar = map[string]string{}
	}
	resultJson, _ := json.Marshal(sliceOfMaps)
	return resultJson, sliceOfMaps
}

func main() {

	log.Println("Инициализация")
	os.Setenv("rac", "C:/Program Files/1cv8/8.3.16.1063/bin/rac.exe")

	LogFile, err := os.OpenFile("logfile.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0777)
	if err != nil {
		log.Println("Ошибка создания log файла")
		panic(err)
	}
	defer LogFile.Close()

	jsonFile, err := os.Open("settings.json")
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("Successfully Opened settings.json")
	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	var srv Server

	if err := json.Unmarshal(byteValue, &srv); err != nil {
		log.Println("Ошибка настройка")
		panic(err)
	}

	os.Setenv("rac", srv.PathToRac)
	var addr = flag.String("listen-address", ":"+srv.ExporterPort, "The address to listen on for HTTP requests.")

	fmt.Println(srv)

	//log.SetOutput(LogFile)

	ScheduledJobsDeny := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ScheduledJobsDeny_status",
			Help: "Блокировка регламентных",
		},
		[]string{"ibname"},
	)
	prometheus.MustRegister(ScheduledJobsDeny)

	out, _ := cmdExec(os.Getenv("rac"), "cluster", "list")
	if err := json.Unmarshal(out, &srv.Cluster); err != nil {
		log.Println("Ошибка разбора списка кластеров")
		panic(err)
	}

	out, _ = cmdExec(os.Getenv("rac"), "infobase", "summary", "list", "--cluster="+srv.Cluster[0].Cluster)
	if out != nil {
		if err := json.Unmarshal(out, &srv.Cluster[0].Infobase); err != nil {
			log.Println("Ошибка разбора списка баз")
			panic(err)
		}
	}

	log.Println("Получение сведений о списке баз")

	go func() {
		for {
			for j := range srv.Cluster[0].Infobase {
				if srv.Cluster[0].Infobase[j].User != ""{
					//log.Println("Получаем данные о ", srv.Cluster[0].Name, "- >", srv.Cluster[0].Infobase[j].Name)
					out, sliceOfMaps0 := cmdExec(os.Getenv("rac"), "infobase", "info", "--cluster="+srv.Cluster[0].Cluster, "--infobase="+srv.Cluster[0].Infobase[j].Infobase, "--infobase-user="+srv.Cluster[0].Infobase[j].User, "--infobase-pwd="+srv.Cluster[0].Infobase[j].Password)
					if out != nil {
						out, _ = json.Marshal(sliceOfMaps0[0])
						if err := json.Unmarshal(out, &srv.Cluster[0].Infobase[j]); err != nil {
							panic(err)
						}
					} else {
						continue
					}
					if srv.Cluster[0].Infobase[j].ScheduledJobsDeny == "on"{
						ScheduledJobsDeny.With(prometheus.Labels{"ibname": srv.Cluster[0].Infobase[j].Name}).Set(1)
					} else {
						ScheduledJobsDeny.With(prometheus.Labels{"ibname": srv.Cluster[0].Infobase[j].Name}).Set(0)
					}

				} else {
					continue
				}

			}
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	log.Printf("Starting web server at %s\n", *addr)
	err = http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Printf("http.ListenAndServer: %v\n", err)
	}
}
