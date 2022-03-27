package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/costexplorer"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/wcharczuk/go-chart"
)

type Record struct {
	Name  string
	Cost  float64
	Ratio float64
}

type discordImage struct {
	URL string `json:"url"`
	H   int    `json:"height"`
	W   int    `json:"width"`
}

type discordAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Icon string `json:"icon_url"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbed struct {
	Title  string         `json:"title"`
	Desc   string         `json:"description"`
	URL    string         `json:"url"`
	Color  int            `json:"color"`
	Image  discordImage   `json:"image"`
	Thumb  discordImage   `json:"thumbnail"`
	Author discordAuthor  `json:"author"`
	Fields []discordField `json:"fields"`
}

type discordWebhook struct {
	Username string         `json:"username"`
	Content  string         `json:"content"`
	Embeds   []discordEmbed `json:"embeds"`
}

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		fmt.Println("cannot load .env file", err)
	}

	AWS_ACCOUNT := os.Getenv("AWS_ACCOUNT")
	BOT_TOKEN := os.Getenv("BOT_TOKEN")
	CHANNEL_ID := os.Getenv("CHANNEL_ID")

	start, end := getTimePeriod()
	res, err := getCost(start, end)
	log.Printf("%v", res)
	if err != nil {
		fmt.Errorf("%w", err)
		return
	}

	// Calculate the total aws usage costs
	total := getTotalCost(res)

	// Get the list in descending order with respect to service usage costs
	list := createCostList(res, total)

	content := fmt.Sprintf("AWS Account: %v\nTimePeriod: %v - %v\nTotal: $%.2f\n\n", AWS_ACCOUNT, start, end, total)

	for _, v := range list {
		content = content + fmt.Sprintf("- %v: $%.2f (%.1f%%)\n", v.Name, v.Cost, v.Ratio)
	}

	b, err := drawPieChart(list)
	if err != nil {
		fmt.Println("error creating Pie Chart", err)
		return
	}

	dg, err := discordgo.New("Bot " + BOT_TOKEN)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}

	st, err := dg.WebhookCreate(CHANNEL_ID, "aws", "aws")
	if err != nil {
		fmt.Println("error creating Webhook,", err)
		return
	}

	file := &discordgo.File{
		Name:        "test.png",
		ContentType: "image/png",
		Reader:      b,
	}

	var files []*discordgo.File
	files = append(files, file)

	data := &discordgo.WebhookParams{
		Username: st.Name,
		Content:  fmt.Sprintf("__Daily Report__\n\n%v", content),
		Files:    files,
	}
	_, err = dg.WebhookExecute(st.ID, st.Token, true, data)
	if err != nil {
		fmt.Println("error executing Webhook,", err)
		return
	}

	err = dg.WebhookDelete(st.ID)
	if err != nil {
		fmt.Println("error delete Webhook", err)
		return
	}
}

func drawPieChart(records []Record) (*bytes.Buffer, error) {
	var values []chart.Value
	others := chart.Value{
		Label: "Others",
		Value: 0.0,
	}

	for _, v := range records {
		if v.Ratio < 1.0 {
			others.Value += v.Cost
			continue
		}
		values = append(values, chart.Value{
			Value: v.Cost,
			Label: v.Name,
		})
	}
	values = append(values, others)

	c := chart.PieChart{
		Width:  512,
		Height: 512,
		Values: values,
	}

	buffer := bytes.NewBuffer([]byte{})
	err := c.Render(chart.PNG, buffer)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	return buffer, nil
}

func getTotalCost(cost *costexplorer.GetCostAndUsageOutput) float64 {
	total := 0.0

	for _, v := range cost.ResultsByTime[0].Groups {
		x, _ := strconv.ParseFloat(*v.Metrics["BlendedCost"].Amount, 64)
		total = total + x
	}

	return total
}

func createCostList(cost *costexplorer.GetCostAndUsageOutput, total float64) []Record {
	res := make([]Record, 0, 0)

	for _, v := range cost.ResultsByTime[0].Groups {
		cost, _ := strconv.ParseFloat(*v.Metrics["BlendedCost"].Amount, 64)
		res = append(res, Record{
			Name:  *v.Keys[0],
			Cost:  cost,
			Ratio: cost / total * 100,
		})
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].Cost > res[j].Cost
	})

	return res
}

func getCost(start, end string) (*costexplorer.GetCostAndUsageOutput, error) {
	timePeriod := costexplorer.DateInterval{
		Start: aws.String(start),
		End:   aws.String(end),
	}
	granularity := "MONTHLY"
	metrics := []string{"BlendedCost"}
	group := costexplorer.GroupDefinition{
		Type: aws.String("DIMENSION"),
		Key:  aws.String("SERVICE"),
	}

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1"),
	})

	if err != nil {
		log.Println("%v", err)
		return nil, fmt.Errorf("%w", err)
	}

	svc := costexplorer.New(sess)

	res, err := svc.GetCostAndUsage(&costexplorer.GetCostAndUsageInput{
		TimePeriod:  &timePeriod,
		Granularity: aws.String(granularity),
		GroupBy:     []*costexplorer.GroupDefinition{&group},
		Metrics:     aws.StringSlice(metrics),
	})

	if err != nil {
		log.Printf("%v", err)
		return nil, fmt.Errorf("%w", err)
	}

	return res, err
}

func getTimePeriod() (string, string) {
	jst, _ := time.LoadLocation("Asia/Tokyo")
	today := time.Now().UTC().In(jst)
	startDayOfThisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, jst)
	start := startDayOfThisMonth.Format("2006-01-02")
	end := today.Format("2006-01-02")

	return start, end
}
