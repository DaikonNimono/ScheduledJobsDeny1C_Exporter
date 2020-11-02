package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/text/encoding/charmap"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Server struct {
	PathToRac    string `json:"path_to_rac"`
	ExporterPort string `json:"exporter_port"`
	Delay        int    `json:"delay"`
	Cluster      []struct {
		Cluster  string `json:"cluster"`
		Name     string `json:"name"`
		Infobase []struct {
			Infobase          string `json:"infobase"`
			User              string `json:"user"`
			Name              string `json:"name"`
			Password          string `json:"password"`
			ScheduledJobsDeny string `json:"scheduled-jobs-deny"`
			SessionsDeny      string `json:"sessions-deny"`
			InfobaseInfoAgrs  []string
		} `json:"infobase"`
	} `json:"cluster"`
}

var SessionsDeny = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "SessionsDeny_status",
		Help: "Блокировка начала сеансов",
	},
	[]string{"ibname"},
)
var ScheduledJobsDeny = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "ScheduledJobsDeny_status",
		Help: "Блокировка регламентных",
	},
	[]string{"ibname"},
)

//Список кластеров
func (srv Server) ClusterList() {
	ClusterList := []string{"cluster", "list"}
	out, _ := cmdExec(os.Getenv("rac"), ClusterList...)
	if err := json.Unmarshal(out, &srv.Cluster); err != nil {
		log.Println("Ошибка разбора списка кластеров")
		panic(err)
	}
}

//Проверка файла настроек
func (srv Server) CheckSettings() {
	for i := range srv.Cluster {
		for j := range srv.Cluster[i].Infobase {
			if srv.Cluster[i].Infobase[j].User == "" || srv.Cluster[i].Infobase[j].Name == "" {
				panic("Незаполнен пользователь или имя информационной базы")
			}
		}
	}
}

//Список баз в кластере
func (srv Server) InfobaseSummary() {
	//log.Println("Получение сведений о списке баз", srv.Cluster[0].Name, srv.Cluster[0].Cluster)
	out, _ := cmdExec(os.Getenv("rac"), srv.GetArgs("SummaryList")...)
	if out != nil {
		if err := json.Unmarshal(out, &srv.Cluster[0].Infobase); err != nil {
			log.Println("Ошибка разбора списка баз")
			panic(err)
		}
	}
}

//Подробная информация о конкретной базе
func (srv Server) InfobaseInfo() {
	for j := range srv.Cluster[0].Infobase {
		out, sliceOfMaps0 := cmdExec(os.Getenv("rac"), srv.Cluster[0].Infobase[j].InfobaseInfoAgrs...)
		out, _ = json.Marshal(sliceOfMaps0[0])
		if err := json.Unmarshal(out, &srv.Cluster[0].Infobase[j]); err != nil {
			panic(err)
		}
		if srv.Cluster[0].Infobase[j].ScheduledJobsDeny == "on" {
			ScheduledJobsDeny.With(prometheus.Labels{"ibname": srv.Cluster[0].Infobase[j].Name}).Set(1)
		} else {
			ScheduledJobsDeny.With(prometheus.Labels{"ibname": srv.Cluster[0].Infobase[j].Name}).Set(0)
		}
		if srv.Cluster[0].Infobase[j].SessionsDeny == "on" {
			SessionsDeny.With(prometheus.Labels{"ibname": srv.Cluster[0].Infobase[j].Name}).Set(1)
		} else {
			SessionsDeny.With(prometheus.Labels{"ibname": srv.Cluster[0].Infobase[j].Name}).Set(0)
		}
	}
}

func (srv Server) ShowSummary() {
	for j := range srv.Cluster {
		log.Println("Кластер (uuid):", srv.Cluster[j].Name, srv.Cluster[j].Cluster)
		for i := range srv.Cluster[j].Infobase {
			log.Println("\tИмя базы (uuid): ", srv.Cluster[j].Infobase[i].Name, srv.Cluster[j].Infobase[i].Infobase)
			log.Println("\t\t Блокировка регламентных", srv.Cluster[j].Infobase[i].ScheduledJobsDeny)
			log.Println("\t\t Блокировка начала сеанса", srv.Cluster[j].Infobase[i].SessionsDeny)
		}
	}
}

//Составление аргументов для методов
func (srv Server) GetArgs(arg string) []string {
	switch arg {
	case "SummaryList":
		SummaryList := []string{"infobase", "summary", "list", "--cluster=" + srv.Cluster[0].Cluster}
		return SummaryList
	case "ClusterList":
		ClusterList := []string{"cluster", "list"}
		return ClusterList
	}
	return nil
}
func (srv Server) GetInfobaseInfoAgrs() {
	for j := range srv.Cluster[0].Infobase {
		srv.Cluster[0].Infobase[j].InfobaseInfoAgrs = []string{"infobase", "info", "--cluster=" + srv.Cluster[0].Cluster, "--infobase=" + srv.Cluster[0].Infobase[j].Infobase, "--infobase-user=" + srv.Cluster[0].Infobase[j].User, "--infobase-pwd=" + srv.Cluster[0].Infobase[j].Password}
	}
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

	jsonFile, err := os.Open("settings.json")
	if err != nil {
		fmt.Println(err)
	}
	log.Println("Successfully Opened settings.json")
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)

	LogFile, err := os.OpenFile("logfile.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0777)
	if err != nil {
		log.Println("Ошибка создания log файла")
		panic(err)
	}
	defer LogFile.Close()

	var srv Server
	if err := json.Unmarshal(byteValue, &srv); err != nil {
		log.Println("Ошибка получения настроек программы")
		panic(err)
	}
	srv.CheckSettings()

	log.Printf("Starting web server at %s\n", srv.ExporterPort)
	log.SetOutput(LogFile)
	os.Setenv("rac", srv.PathToRac)
	prometheus.MustRegister(ScheduledJobsDeny, SessionsDeny)

	srv.ClusterList()
	srv.InfobaseSummary()
	srv.GetInfobaseInfoAgrs()
	srv.InfobaseInfo()
	srv.ShowSummary()

	fmt.Println("Done")

	var addr = flag.String("listen-address", ":"+srv.ExporterPort, "The address to listen on for HTTP requests.")

	go func() {
		for {
			srv.InfobaseInfo()
			log.Println("sleep")
			time.Sleep(time.Duration(srv.Delay) * time.Second)
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	log.Printf("Starting web server at %s\n", *addr)
	err = http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Printf("http.ListenAndServer: %v\n", err)
	}
}
