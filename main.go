package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gertv/go-freckle"
	"github.com/samuel/go-librato/librato"
)

const (
	freckleAppName      = "DoesNotMatter"
	freckleTokenVarName = "FRECKLE_APP_TOKEN"

	libratoAccountVarName        = "LIBRATO_ACCOUNT"
	libratoTokenVarName          = "LIBRATO_TOKEN"
	libratoBaseName              = "FreckleAPI"
	libratoCatProjects           = "projects"
	libratoCatParticipants       = "participants"
	libratoCatYearlyParticipants = "yearlyParticipants"
)
const (
	exitCodeOk = iota
	exitCodeNotOk
)

func sanitizeMetricName(s string) string {
	s = strings.Replace(s, " ", "-", -1)
	s = strings.Replace(s, "/", "-", -1)
	s = strings.Replace(s, "\\", "-", -1)
	s = strings.Replace(s, "#", "", -1)
	s = strings.Replace(s, "(", "", -1)
	s = strings.Replace(s, ")", "", -1)
	return s
}

// ParticipantKpi represents a freckle Participant enriched with Billable and Unbillable information.
type ParticipantKpi struct {
	freckle.Participant

	BillableMinutes   int
	UnbillableMinutes int
}

func (p ParticipantKpi) String() string {
	return fmt.Sprintf(
		"%s Billable : %.1fh - Unbillable : %.1fh",
		p.Email,
		float64(p.BillableMinutes)/60,
		float64(p.UnbillableMinutes)/60,
	)

}

// VerboseString prints detailed information for a Participant in the context of a project.
func (p ParticipantKpi) VerboseString(prj ProjectKpi) string {
	billablePercent := float64(p.BillableMinutes) / float64(prj.BillableMinutes) * 100
	unbillablePercent := float64(p.UnbillableMinutes) / float64(prj.UnbillableMinutes) * 100
	return fmt.Sprintf(
		"%s Billable : %.1fh (%f %%) - Unbillable : %.1fh (%f %%)",
		p.Email,
		float64(p.BillableMinutes)/60, billablePercent,
		float64(p.UnbillableMinutes)/60, unbillablePercent,
	)
}

// RegisterMetrics regiters project metrics and update their value
func (p ParticipantKpi) RegisterMetrics(m *librato.Metrics, prefix, source string) {
	source = sanitizeMetricName(source)

	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.UnbillableMinutes.%s-%s", prefix, p.FirstName, p.LastName),
			Source: source,
			Count:  1,
			Sum:    float64(p.UnbillableMinutes),
		})

	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.BillableMinutes.%s-%s", prefix, p.FirstName, p.LastName),
			Source: source,
			Count:  1,
			Sum:    float64(p.BillableMinutes),
		})
}

// GetParticipantKpis calculates slice of ParticipantKpi based on a slice of Freckle Entry.
func GetParticipantKpis(fes []freckle.Entry) ParticipantKpis {
	participantsMap := make(map[int]ParticipantKpi)
	var user freckle.Participant
	for _, entry := range fes {
		user = entry.User
		pkpi, ok := participantsMap[user.Id]
		if !ok {
			pkpi = ParticipantKpi{user, 0, 0}
		}
		if entry.Billable {
			pkpi.BillableMinutes += entry.Minutes
		} else {
			pkpi.UnbillableMinutes += entry.Minutes
		}
		participantsMap[user.Id] = pkpi

	}

	var pks ParticipantKpis
	for _, v := range participantsMap {
		pks = append(pks, v)
	}
	sort.Sort(sort.Reverse(pks))
	return pks
}

// ParticipantKpis is a type alias on which we are going to implement the methods required by the Sort interface.
type ParticipantKpis []ParticipantKpi

func (slice ParticipantKpis) Len() int {
	return len(slice)
}

func (slice ParticipantKpis) Less(i, j int) bool {
	return slice[i].BillableMinutes+slice[i].UnbillableMinutes < slice[j].BillableMinutes+slice[j].UnbillableMinutes
}

func (slice ParticipantKpis) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

// TimeAggregater represents the set of method we need to extract information from time.Time
type TimeAggregater interface {
	GetInt(time.Time) (int, error)
	GetPeriod(time.Time) time.Time
	GetString(time.Time) string
}

// MonthAgg reprents a monthly TimeAggregater
type MonthAgg struct{}

// GetInt returns the int composed by the Year and a double digit Month
func (m MonthAgg) GetInt(t time.Time) (int, error) {
	return strconv.Atoi(fmt.Sprintf("%d%02d", t.Year(), t.Month()))
}

// GetString returns the string composed by the Year and a double digit Month separated by a `-`
func (m MonthAgg) GetString(t time.Time) string {
	return fmt.Sprintf("%d-%02d", t.Year(), t.Month())
}

// GetPeriod returns the time.Time truncated after the Year and Month
func (m MonthAgg) GetPeriod(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// YearAgg reprents a monthly TimeAggregater
type YearAgg struct{}

// GetInt returns the int composed by the Year
func (y YearAgg) GetInt(t time.Time) (int, error) {
	return strconv.Atoi(fmt.Sprintf("%d", t.Year()))
}

// GetString returns the string composed by the Year
func (y YearAgg) GetString(t time.Time) string {
	return fmt.Sprintf("%d", t.Year())
}

// GetPeriod returns the time.Time truncated after the Year
func (y YearAgg) GetPeriod(t time.Time) time.Time {
	return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
}

// InvoicePeriodKpi is used to aggregate invoice information on a period
type InvoicePeriodKpi struct {
	TimeAgg TimeAggregater
	Period  time.Time
	Amount  float64
}

func (ik InvoicePeriodKpi) String() string {
	return fmt.Sprintf("%s $%.2f invoiced", ik.TimeAgg.GetString(ik.Period), ik.Amount)
}

// GetInvoiceKpiPerPeriod calculates a slice of InvoicePeriodKpi based on a slice of freckle invoice.
func GetInvoiceKpiPerPeriod(tagg TimeAggregater, fis []freckle.Invoice) ([]InvoicePeriodKpi, error) {
	agrregateInvoices := make(map[int]InvoicePeriodKpi)
	var keys []int
	var key int
	for _, invoice := range fis {
		t, err := time.Parse("2006-01-02", invoice.InvoiceDate)
		if err != nil {
			return nil, err
		}
		key, err = tagg.GetInt(t)
		if err != nil {
			return nil, err
		}

		ik, ok := agrregateInvoices[key]
		if !ok {
			keys = append(keys, key)
		}
		ik.Period = tagg.GetPeriod(t)
		ik.Amount += invoice.TotalAmount
		ik.TimeAgg = tagg

		agrregateInvoices[key] = ik
	}
	sort.Ints(keys)

	var sik []InvoicePeriodKpi
	for _, v := range keys {
		sik = append(sik, agrregateInvoices[v])
	}
	return sik, nil
}

// GetInvoiceKpiPerMonth calculates a slice of InvoicePeriodKpi based on a slice of freckle invoice.
func GetInvoiceKpiPerMonth(fis []freckle.Invoice) ([]InvoicePeriodKpi, error) {
	return GetInvoiceKpiPerPeriod(MonthAgg{}, fis)
}

// GetInvoiceKpiPerYear calculates a slice of InvoicePeriodKpi based on a slice of freckle invoice.
func GetInvoiceKpiPerYear(fis []freckle.Invoice) ([]InvoicePeriodKpi, error) {
	return GetInvoiceKpiPerPeriod(YearAgg{}, fis)
}

// ParticipantsPeriod is used to aggregate a list of ParticipantKpi over a period.
type ParticipantsPeriod struct {
	TimeAgg      TimeAggregater
	Period       time.Time
	Participants ParticipantKpis
}

// GetParticipantsPeriodPerPeriod Builds a slice of ParticipantsPeriod over the period of the given freckle entries.
func GetParticipantsPeriodPerPeriod(tagg TimeAggregater, fes []freckle.Entry) ([]ParticipantsPeriod, error) {
	dedupParticipants := make(map[int]ParticipantsPeriod)
	var keys []int
	var key int
	for _, entry := range fes {
		// TODO: shall we use entry.invoiceAt
		t, err := time.Parse("2006-01-02", entry.Date)
		if err != nil {
			return nil, err
		}
		key, err = tagg.GetInt(t)
		if err != nil {
			return nil, err
		}

		pk, ok := dedupParticipants[key]
		if !ok {
			keys = append(keys, key)
		}
		pk.Period = tagg.GetPeriod(t)
		pk.TimeAgg = tagg

		// Check if the ParticipantKpi already exist in the slice
		foundFlag := false
		for i, p := range pk.Participants {
			if p.Id == entry.User.Id {
				if entry.Billable {
					p.BillableMinutes += entry.Minutes
				} else {
					p.UnbillableMinutes += entry.Minutes
				}
				pk.Participants[i] = p
				foundFlag = true
				break
			}
		}
		if !foundFlag {
			var billableMinutes, unbillableMinutes int
			if entry.Billable {
				billableMinutes = entry.Minutes
			} else {
				unbillableMinutes = entry.Minutes
			}
			pk.Participants = append(pk.Participants, ParticipantKpi{entry.User, billableMinutes, unbillableMinutes})
		}
		dedupParticipants[key] = pk
	}

	var participants []ParticipantsPeriod
	for _, v := range dedupParticipants {
		sort.Sort(sort.Reverse(v.Participants))
		participants = append(participants, v)
	}
	return participants, nil
}

// GetParticipantsPeriodPerMonth Builds a slice of ParticipantsPeriod over months for the given freckle entries.
func GetParticipantsPeriodPerMonth(fes []freckle.Entry) ([]ParticipantsPeriod, error) {
	return GetParticipantsPeriodPerPeriod(MonthAgg{}, fes)
}

// GetParticipantsPeriodPerYear Builds a slice of ParticipantsPeriod over years for the given freckle entries.
func GetParticipantsPeriodPerYear(fes []freckle.Entry) ([]ParticipantsPeriod, error) {
	return GetParticipantsPeriodPerPeriod(YearAgg{}, fes)
}

// ProjectKpi is a freckle project enriched with the related entries
type ProjectKpi struct {
	freckle.Project
	DetailedEntries []freckle.Entry
}

// GetInvoicedTotal return the grand total of amount invoiced
func (pi *ProjectKpi) GetInvoicedTotal() float64 {
	invoicedAmount := 0.0
	for _, invoice := range pi.Invoices {
		invoicedAmount += invoice.TotalAmount
	}
	return invoicedAmount
}

func (pi ProjectKpi) String() string {
	invoiced := pi.GetInvoicedTotal()
	billableHours := float64(pi.BillableMinutes) / 60
	hourlyRate := invoiced / billableHours
	invoicedHours := float64(pi.InvoicedMinutes) / 60
	invoicedHourlyRate := invoiced / invoicedHours
	return fmt.Sprintf(
		"%s total invoiced : $%.2f, %.1fh (%.1f$/h) - Billable : %.1fh (%.1f$/h) - Unbillable : %.1fh",
		pi.Name,
		invoiced, invoicedHours, invoicedHourlyRate,
		billableHours, hourlyRate,
		float64(pi.UnbillableMinutes)/60)
}

// RegisterMetrics registers project metrics and set their value
func (pi ProjectKpi) RegisterMetrics(m *librato.Metrics) {
	prjName := sanitizeMetricName(pi.Name)

	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.%s.UnbillableMinutes", libratoBaseName, libratoCatProjects),
			Source: prjName,
			Count:  1,
			Sum:    float64(pi.UnbillableMinutes),
		})

	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.%s.BillableMinutes", libratoBaseName, libratoCatProjects),
			Source: prjName,
			Count:  1,
			Sum:    float64(pi.BillableMinutes),
		})

	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.%s.InvoicedMinutes", libratoBaseName, libratoCatProjects),
			Source: prjName,
			Count:  1,
			Sum:    float64(pi.InvoicedMinutes),
		})

	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.%s.InvoicedAmount", libratoBaseName, libratoCatProjects),
			Source: prjName,
			Count:  1,
			Sum:    float64(pi.GetInvoicedTotal()),
		})
}

// ProjectPeriodKpi represents the project information for a period.
type ProjectPeriodKpi struct {
	Name         string
	TimeAgg      TimeAggregater
	Period       time.Time
	Invoice      InvoicePeriodKpi
	Participants []ParticipantKpi
}

func (pp ProjectPeriodKpi) String() string {
	return fmt.Sprintf("%s $%.2f invoiced", pp.TimeAgg.GetString(pp.Period), pp.Invoice.Amount)
}

// RegisterMetrics registers project metrics and update their value
func (pp ProjectPeriodKpi) RegisterMetrics(m *librato.Metrics, prefix string) {
	prjName := sanitizeMetricName(pp.Name)
	source := pp.TimeAgg.GetString(pp.Period)

	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.InvoicedAmount.%s", prefix, prjName),
			Source: source,
			Count:  1,
			Sum:    float64(pp.Invoice.Amount),
		})

	var billableMin int
	var unbillableMin int
	for _, p := range pp.Participants {
		billableMin += p.BillableMinutes
		unbillableMin += p.UnbillableMinutes
	}
	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.UnbillableMinutes.%s", prefix, prjName),
			Source: source,
			Count:  1,
			Sum:    float64(unbillableMin),
		})

	m.Gauges = append(m.Gauges,
		librato.Gauge{
			Name:   fmt.Sprintf("%s.BillableMinutes.%s", prefix, prjName),
			Source: source,
			Count:  1,
			Sum:    float64(billableMin),
		})
}

// GetProjectKpiPerPeriod returns the slice of ProjectPeriodKpi.
func GetProjectKpiPerPeriod(tagg TimeAggregater, p ProjectKpi) ([]ProjectPeriodKpi, error) {
	invoiceAmonthPerPeriod, err := GetInvoiceKpiPerPeriod(tagg, p.Invoices)
	if err != nil {
		return nil, err
	}

	participantKpiPerPeriod, err := GetParticipantsPeriodPerPeriod(tagg, p.DetailedEntries)
	if err != nil {
		return nil, err
	}

	mapProjectKpiPerMonth := make(map[int]ProjectPeriodKpi)
	var keys []int
	var key int

	// Acummulates the invoices for the ProjectKpi per period
	for _, invoice := range invoiceAmonthPerPeriod {
		key, err = invoice.TimeAgg.GetInt(invoice.Period)
		if err != nil {
			return nil, err
		}
		ppm, ok := mapProjectKpiPerMonth[key]
		if !ok {
			keys = append(keys, key)
		}
		ppm.Name = p.Name
		ppm.TimeAgg = tagg
		ppm.Period = invoice.Period
		ppm.Invoice = invoice
		mapProjectKpiPerMonth[key] = ppm
	}

	// Accumulates the particpants for the ProjectKpi per period
	for _, participants := range participantKpiPerPeriod {
		key, err = participants.TimeAgg.GetInt(participants.Period)
		if err != nil {
			return nil, err
		}
		ppm, ok := mapProjectKpiPerMonth[key]
		if !ok {
			keys = append(keys, key)
		}
		ppm.Name = p.Name
		ppm.TimeAgg = tagg
		ppm.Period = participants.Period
		ppm.Participants = participants.Participants
		mapProjectKpiPerMonth[key] = ppm
	}

	// returns the sorted slice of ProjectPeriodKpi
	sort.Ints(keys)
	var projectsPeriod []ProjectPeriodKpi
	for _, v := range keys {
		projectsPeriod = append(projectsPeriod, mapProjectKpiPerMonth[v])
	}
	return projectsPeriod, nil
}

// GetProjectKpiPerMonth returns the slice of ProjectPeriodKpi.
func GetProjectKpiPerMonth(p ProjectKpi) ([]ProjectPeriodKpi, error) {
	return GetProjectKpiPerPeriod(MonthAgg{}, p)
}

// GetProjectKpiPerYear returns the slice of ProjectPeriodKpi.
func GetProjectKpiPerYear(p ProjectKpi) ([]ProjectPeriodKpi, error) {
	return GetProjectKpiPerPeriod(YearAgg{}, p)
}

var (
	libratoFlag bool
	timeAggFlag string
)

func init() {
	flag.BoolVar(&libratoFlag, "librato", false, "Push metrics to librato")
	flag.StringVar(&timeAggFlag, "period", "year", "Time period you want to build the aggregation on : month, year")
}

func main() {
	flag.Parse()
	// Grab Freckle app token from the environment
	freckleAppToken := os.Getenv(freckleTokenVarName)
	if freckleAppToken == "" {
		fmt.Println(freckleTokenVarName, "environment variable is not set")
		os.Exit(exitCodeNotOk)
	}

	f := freckle.LetsFreckle(freckleAppName, freckleAppToken)
	//f.Debug(true)

	libratoAccount := os.Getenv(libratoAccountVarName)
	libratoToken := os.Getenv(libratoTokenVarName)
	metrics := &librato.Metrics{
		Counters: []librato.Metric{},
		Gauges:   []interface{}{},
	}

	var projects []ProjectKpi

	projectsPage, err := f.ProjectsAPI().ListProjects(
		func(p freckle.Parameters) {})
	if err != nil {
		fmt.Println("An error occurred while getting the project list:\n\t", err)
	}

	projectNames := flag.Args()
	stopWhenZero := len(projectNames)

	for project := range projectsPage.AllProjects() {
		if len(projectNames) > 0 && stopWhenZero > 0 {
			isInProjectList := false
			for _, name := range projectNames {
				if name == project.Name {
					stopWhenZero--
					isInProjectList = true
				}
			}
			if !isInProjectList {
				continue
			}
		} else if len(projectNames) > 0 && stopWhenZero == 0 {
			break
		}

		var entries []freckle.Entry
		entriesPage, err := f.ProjectsAPI().GetEntries(project.Id)
		if err != nil {
			log.Fatal(err)
		}
		for entry := range entriesPage.AllEntries() {
			entries = append(entries, entry)
		}

		projectKpi := ProjectKpi{project, entries}
		projects = append(projects, projectKpi)
	}

	for _, project := range projects {
		// Print out the project information
		fmt.Println(project.String())
		project.RegisterMetrics(metrics)

		for _, p := range GetParticipantKpis(project.DetailedEntries) {
			fmt.Println("\t", p.VerboseString(project))
			p.RegisterMetrics(
				metrics,
				fmt.Sprintf("%s.%s", libratoBaseName, libratoCatParticipants),
				project.Name)
		}

		switch timeAggFlag {
		case "month":
			projectKpiPerPeriod, err := GetProjectKpiPerMonth(project)
			if err != nil {
				log.Fatal(err)
			}

			// Print out the per month information
			fmt.Println("\n\tbreakdown per month")
			for _, ppm := range projectKpiPerPeriod {
				fmt.Println("\t\t", ppm.String())
				for _, participant := range ppm.Participants {
					fmt.Println("\t\t\t", participant.String())
				}
			}
		case "year":

			projectKpiPerPeriod, err := GetProjectKpiPerYear(project)
			if err != nil {
				log.Fatal(err)
			}

			// Print out the per year information
			fmt.Println("\n\tbreakdown per year")
			for _, ppm := range projectKpiPerPeriod {
				fmt.Println("\t\t", ppm.String())
				ppm.RegisterMetrics(
					metrics,
					fmt.Sprintf("%s.%s", libratoBaseName, libratoCatYearlyParticipants))
				for _, participant := range ppm.Participants {
					fmt.Println("\t\t\t", participant.String())
				}
			}
		default:
			fmt.Println("\nTime period options are : month or year")
			fmt.Println(timeAggFlag, "is not a valid choice.")
			os.Exit(exitCodeNotOk)
		}
	}

	// Only report to librato if we found the environment variables
	if libratoFlag && libratoAccount != "" && libratoToken != "" {
		libratoClient := &librato.Client{Username: libratoAccount, Token: libratoToken}
		err := libratoClient.PostMetrics(metrics)
		if err != nil {
			fmt.Println("An error occured while POSTing the metrics to librato", err)
		}
	}
}
