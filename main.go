package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/costexplorer"

	"github.com/bwmarrin/discordgo"
	"github.com/wcharczuk/go-chart"
)

type Record struct {
	Name  string
	Cost  float64
	Ratio float64
}

type Payload struct {
	content string
	file    *bytes.Buffer
}

func main() {
	AWS_ACCOUNT_ID := os.Getenv("AWS_ACCOUNT")
	BOT_TOKEN := os.Getenv("BOT_TOKEN")
	CHANNEL_ID := os.Getenv("CHANNEL_ID")

	start, end := getTimePeriod()
	res, err := getCost(start, end)
	if err != nil {
		fmt.Errorf("cannot get cost, %v", err)
	}

	// Calculate the total aws usage costs
	total := getTotalCost(res)

	// Get the list in descending order with respect to service usage costs
	list := createCostList(res, total)

	content := createContent(total, list, start, end, AWS_ACCOUNT_ID)

	buffer, err := drawPieChart(list)
	if err != nil {
		fmt.Errorf("error create Pie Chart, %v", err)
	}

	payload := Payload{content, buffer}

	message, err := sendCost(payload, BOT_TOKEN, CHANNEL_ID)
	if err != nil {
		fmt.Errorf("cannot send payload, %v", err)
	}

	fmt.Println(message)
}

func createContent(total float64, list []Record, start, end, AWS_ACCOUNT_ID string) string {
	content := fmt.Sprintf("__Daily Report__\n\nAWS Account: %v\nTimePeriod: %v - %v\nTotal: $%.2f\n\n", AWS_ACCOUNT_ID, start, end, total)

	for _, v := range list {
		content = content + fmt.Sprintf("- %v: $%.2f (%.1f%%)\n", v.Name, v.Cost, v.Ratio)
	}

	return content
}

func sendCost(payload Payload, BOT_TOKEN, CHANNEL_ID string) (*discordgo.Message, error) {
	dg, err := discordgo.New("Bot " + BOT_TOKEN)
	if err != nil {
		return nil, fmt.Errorf("cannot create Discord session, %v", err)
	}

	webhook, err := dg.WebhookCreate(CHANNEL_ID, "aws", "aws")
	if err != nil {
		return nil, fmt.Errorf("cannot create Webhook, %v", err)
	}

	files := []*discordgo.File{
		{
			Name:        "test.png",
			ContentType: "image/png",
			Reader:      payload.file,
		},
	}

	params := &discordgo.WebhookParams{
		Username: webhook.Name,
		Content:  payload.content,
		Files:    files,
	}

	message, err := dg.WebhookExecute(webhook.ID, webhook.Token, true, params)
	if err != nil {
		return nil, fmt.Errorf("error execute Webhook, %v", err)
	}

	err = dg.WebhookDelete(webhook.ID)
	if err != nil {
		return nil, fmt.Errorf("error delete Webhook, %v", err)
	}

	return message, nil
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
