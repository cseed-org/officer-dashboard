// Package seed holds the sample data for local development and tests: a nonprofit chart of
// accounts mapped to Form 990, funds, fake officers, events with guests, purchase requests,
// a budget and a few balanced journal entries. It reproduces scripts/seed/data.ts exactly.
//
// THE CHART OF ACCOUNTS AND 990 MAPPINGS ARE A STARTING POINT, NOT TAX ADVICE.
// Have CSEED's CPA review them before real books are kept.
package seed

const FiscalYear = 2027 // Jul 2026 – Jun 2027

type User struct{ ID, DiscordID, Name, Role, Title, Email string }

var Users = []User{
	{"user-leadership", "100000000000000005", "Sam Rivera", "leadership", "President", "sam@example.org"},
	{"user-director", "100000000000000001", "Jordan Lee", "director", "Executive Director", "jordan@example.org"},
	{"user-priya", "100000000000000002", "Priya Shah", "officer", "VP Events", "priya@example.org"},
	{"user-marcus", "100000000000000003", "Marcus Chen", "officer", "Treasurer", "marcus@example.org"},
	{"user-ava", "100000000000000004", "Ava Romero", "officer", "BuildHer Lead", "ava@example.org"},
	{"user-riley", "100000000000000006", "Riley Park", "officer", "Design Lead", "riley@example.org"},
}

// DesignRequest is a sample card on the Design Requests board.
type DesignRequest struct {
	ID, Title, Type, Formats, Due, Urgency, Brief, Event, Program, Stage, Requester, Assignee string
}

var DesignRequests = []DesignRequest{
	{"design-hack-poster", "Hack Night poster", "poster", "A3 print PDF; 1080 × 1350 PNG", "2026-11-06", "high", "Invite students to Hack Night. Lead with Nov 13, 6 PM, Engineering Atrium, and include the RSVP QR code.", "event-hack-night", "", "new", "user-ava", ""},
	{"design-mentor-social", "Mentor coffee social", "social", "1080 × 1350 PNG", "2026-11-09", "normal", "An Instagram post for the mentor coffee social. Friendly, warm, room for three mentor headshots.", "", "Mentorship program", "accepted", "user-priya", "user-riley"},
	{"design-kickoff-recap", "Fall Kickoff recap slides", "slides", "16:9 PPTX + PDF", "2026-11-04", "normal", "Five slides recapping Fall Kickoff turnout and photos for the general meeting.", "event-kickoff", "", "in-review", "user-priya", "user-riley"},
}

type Account struct {
	Code, Name, Type, Normal string
	Part, Line, Label        string // "" = none
	Restriction              string
	Budgetable               bool
	Description              string
}

var Accounts = []Account{
	// Assets — Form 990 Part X (balance sheet)
	{Code: "1000", Name: "Checking", Type: "asset", Normal: "debit", Part: "X", Line: "1", Label: "Cash—non-interest-bearing"},
	{Code: "1100", Name: "Accounts receivable", Type: "asset", Normal: "debit", Part: "X", Line: "4", Label: "Accounts receivable, net"},
	{Code: "1150", Name: "Grants & pledges receivable", Type: "asset", Normal: "debit", Part: "X", Line: "3", Label: "Pledges and grants receivable, net"},
	{Code: "1300", Name: "Prepaid expenses", Type: "asset", Normal: "debit", Part: "X", Line: "9", Label: "Prepaid expenses and deferred charges"},
	// Liabilities — Part X
	{Code: "2000", Name: "Accounts payable", Type: "liability", Normal: "credit", Part: "X", Line: "17", Label: "Accounts payable and accrued expenses"},
	{Code: "2100", Name: "Reimbursements payable", Type: "liability", Normal: "credit", Part: "X", Line: "17", Label: "Accounts payable and accrued expenses", Description: "Owed to officers for approved out-of-pocket purchases"},
	// Net assets — Part X, ASC 958 classes
	{Code: "3000", Name: "Net assets without donor restrictions", Type: "net_asset", Normal: "credit", Part: "X", Line: "27", Label: "Net assets without donor restrictions", Restriction: "without_donor"},
	{Code: "3100", Name: "Net assets with donor restrictions", Type: "net_asset", Normal: "credit", Part: "X", Line: "28", Label: "Net assets with donor restrictions", Restriction: "with_donor"},
	// Revenue — Part VIII
	{Code: "4000", Name: "Contributions — individuals", Type: "revenue", Normal: "credit", Part: "VIII", Line: "1f", Label: "All other contributions, gifts, grants"},
	{Code: "4100", Name: "Government grants", Type: "revenue", Normal: "credit", Part: "VIII", Line: "1e", Label: "Government grants (contributions)"},
	{Code: "4110", Name: "Foundation & private grants", Type: "revenue", Normal: "credit", Part: "VIII", Line: "1f", Label: "All other contributions, gifts, grants"},
	{Code: "4200", Name: "Corporate sponsorships", Type: "revenue", Normal: "credit", Part: "VIII", Line: "1f", Label: "All other contributions (qualified sponsorship payments)", Description: "Qualified sponsorship payments; advertising-style deals may be UBI — ask the CPA"},
	{Code: "4300", Name: "Program service revenue", Type: "revenue", Normal: "credit", Part: "VIII", Line: "2a", Label: "Program service revenue", Description: "Event tickets, workshop fees"},
	{Code: "4400", Name: "In-kind contributions", Type: "revenue", Normal: "credit", Part: "VIII", Line: "1f", Label: "All other contributions (noncash; also reported on line 1g)"},
	// Expenses — Part IX (functional split by journal line functional_class)
	{Code: "5100", Name: "Program supplies", Type: "expense", Normal: "debit", Part: "IX", Line: "24a", Label: "Other expenses: program supplies", Budgetable: true},
	{Code: "5200", Name: "Food & events", Type: "expense", Normal: "debit", Part: "IX", Line: "19", Label: "Conferences, conventions, and meetings", Budgetable: true},
	{Code: "5300", Name: "Travel", Type: "expense", Normal: "debit", Part: "IX", Line: "17", Label: "Travel", Budgetable: true},
	{Code: "5400", Name: "Software & IT", Type: "expense", Normal: "debit", Part: "IX", Line: "14", Label: "Information technology", Budgetable: true},
	{Code: "5500", Name: "Printing & publications", Type: "expense", Normal: "debit", Part: "IX", Line: "13", Label: "Office expenses (printing)", Budgetable: true},
	{Code: "5900", Name: "Other expenses", Type: "expense", Normal: "debit", Part: "IX", Line: "24e", Label: "All other expenses", Budgetable: true},
}

// AccountID is the id of the account with a code.
func AccountID(code string) string { return "acct-" + code }

type Donor struct{ ID, Name, Type, Email string }

var Donors = []Donor{
	{"donor-foundation", "Sample Community Foundation", "foundation", "grants@example.org"},
	{"donor-city", "Sample City Office of Economic Development", "government", ""},
	{"donor-acme", "Acme Software (sample sponsor)", "corporate", "sponsorships@example.com"},
	{"donor-alum", "Sam Rivera (sample alum)", "individual", "sam@example.com"},
}

type Fund struct {
	ID, Code, Name, Kind, Restriction, Purpose, DonorID, Start, End string
	AwardedCents                                                    int64 // 0 = none
}

var Funds = []Fund{
	{"fund-general", "GEN", "General operating", "general", "without_donor", "Unrestricted operations", "", "", "", 0},
	{"fund-buildspace", "GR-BUILDSPACE", "Buildspace program grant (sample)", "grant", "with_donor", "Buildspace cohort supplies, food and demo night", "donor-foundation", "2026-08-01", "2027-06-30", 500_000},
	{"fund-buildher", "GR-BUILDHER", "BuildHer workshop grant (sample)", "grant", "with_donor", "BuildHer workshop series for women in tech", "donor-city", "2026-10-01", "2027-05-31", 250_000},
}

type Event struct {
	ID, LumaID, Name, StartsAt, EndsAt, Location, Host string
	Registered                                         int
	CheckInPct, ReflectionPct                          float64
}

var Events = []Event{
	{"event-kickoff", "evt-kickoff26", "Fall Kickoff", "2026-09-18T18:00:00-07:00", "2026-09-18T20:00:00-07:00", "Student Union 210", "user-priya", 64, 0.78, 0.42},
	{"event-resume", "evt-resume26", "Resume Workshop", "2026-10-02T17:30:00-07:00", "2026-10-02T19:00:00-07:00", "Library Lab B", "user-marcus", 38, 0.66, 0.6},
	{"event-buildspace-demo", "evt-bsdemo26", "Buildspace Demo Night", "2026-10-09T18:30:00-07:00", "2026-10-09T21:00:00-07:00", "Engineering Atrium", "user-priya", 92, 0.71, 0.35},
	{"event-buildher-1", "evt-bh1-26", "BuildHer: Intro to Web Apps", "2026-10-16T17:00:00-07:00", "2026-10-16T19:30:00-07:00", "Engineering 120", "user-ava", 30, 0.83, 0.72},
	{"event-founder-talk", "evt-founder26", "Founder Fireside", "2026-10-23T18:00:00-07:00", "2026-10-23T19:30:00-07:00", "Business Hall 300", "user-marcus", 51, 0.55, 0.25},
	{"event-hack-night", "evt-hack26", "Hack Night", "2026-11-13T18:00:00-08:00", "2026-11-13T23:00:00-08:00", "Engineering Atrium", "user-ava", 44, 0, 0},
}

type Decision struct{ Decision, Comment string }

type Request struct {
	ID, Number, Requester, Type, Title, Vendor, ItemURL  string
	Quantity, UnitCostCents, TaxShipCents                int64
	Account, Fund, Event, FunctionalClass, Justification string
	Status, SubmittedAt, DecidedAt                       string
	Decision                                             *Decision
}

var Requests = []Request{
	{"req-0001", "2027-0001", "user-priya", "reimbursement", "Kickoff pizza", "Pagliacci", "", 15, 2450, 3750, "5200", "fund-general", "event-kickoff", "program", "Food for 60 attendees at Fall Kickoff", "reimbursed", "2026-09-12", "2026-09-13", &Decision{"approved", "Within the kickoff budget."}},
	{"req-0002", "2027-0002", "user-ava", "club_purchase", "Arduino starter kits", "Adafruit", "https://www.adafruit.com/", 8, 2195, 1080, "5100", "fund-buildspace", "event-buildspace-demo", "program", "Hardware for Buildspace teams; grant-funded", "fulfilled", "2026-09-20", "2026-09-21", &Decision{"approved", "Charge to the Buildspace grant."}},
	{"req-0003", "2027-0003", "user-marcus", "club_purchase", "Figma education upgrade", "Figma", "https://www.figma.com/education/", 1, 14400, 0, "5400", "fund-general", "", "mgmt_general", "Shared design workspace for officers", "denied", "2026-10-05", "2026-10-06", &Decision{"denied", "Education plan is free; apply for that first."}},
	{"req-0004", "2027-0004", "user-ava", "club_purchase", "BuildHer workshop snacks", "Costco", "", 1, 18500, 1850, "5200", "fund-buildher", "event-hack-night", "program", "Snacks and drinks for 40 people at Hack Night", "approved", "2026-10-27", "2026-10-28", &Decision{"approved", "Approved; keep the receipt."}},
	{"req-0005", "2027-0005", "user-priya", "reimbursement", "Demo night banner printing", "FedEx Office", "", 2, 4200, 860, "5500", "fund-general", "event-buildspace-demo", "program", "Two vinyl banners for Demo Night", "submitted", "2026-10-29", "", nil},
	{"req-0006", "2027-0006", "user-marcus", "club_purchase", "Hack Night prizes", "Amazon", "", 3, 5000, 0, "5900", "fund-general", "event-hack-night", "program", "Gift cards for top three teams", "draft", "", "", nil},
}

type Budget struct {
	Account, Fund string // Fund "" = all funds
	AmountCents   int64
	Notes         string
}

var Budgets = []Budget{
	{"5200", "fund-general", 300_000, "Meeting food, kickoff, socials"},
	{"5100", "fund-general", 80_000, "General supplies"},
	{"5300", "fund-general", 150_000, "Conference travel"},
	{"5400", "fund-general", 60_000, "Domains, hosting, tools"},
	{"5500", "fund-general", 40_000, "Banners, flyers"},
	{"5900", "fund-general", 50_000, "Prizes, misc"},
	{"5100", "fund-buildspace", 300_000, "Grant: hardware and supplies"},
	{"5200", "fund-buildspace", 200_000, "Grant: demo night"},
	{"5200", "fund-buildher", 150_000, "Grant: workshop food"},
	{"5100", "fund-buildher", 100_000, "Grant: workshop materials"},
}

type Line struct {
	Account, Fund string
	Debit, Credit int64
	FC            string
}

type Entry struct {
	ID, Date, Memo, Source, Request string
	Lines                           []Line
}

var Journal = []Entry{
	{"je-0001", "2026-07-01", "Opening balance (sample)", "opening_balance", "", []Line{
		{"1000", "fund-general", 320_000, 0, ""},
		{"3000", "fund-general", 0, 320_000, ""},
	}},
	{"je-0002", "2026-08-15", "Buildspace grant received", "contribution", "", []Line{
		{"1000", "fund-buildspace", 500_000, 0, ""},
		{"4110", "fund-buildspace", 0, 500_000, ""},
	}},
	{"je-0003", "2026-09-05", "Acme sponsorship", "contribution", "", []Line{
		{"1000", "fund-general", 150_000, 0, ""},
		{"4200", "fund-general", 0, 150_000, ""},
	}},
	{"je-0004", "2026-09-15", "Kickoff pizza reimbursement owed (2027-0001)", "purchase_request", "req-0001", []Line{
		{"5200", "fund-general", 40_500, 0, "program"},
		{"2100", "fund-general", 0, 40_500, ""},
	}},
	{"je-0005", "2026-09-16", "Pay reimbursement to Priya Shah (2027-0001)", "purchase_request", "req-0001", []Line{
		{"2100", "fund-general", 40_500, 0, ""},
		{"1000", "fund-general", 0, 40_500, ""},
	}},
	{"je-0006", "2026-09-20", "Gift from Sam Rivera", "contribution", "", []Line{
		{"1000", "fund-general", 30_000, 0, ""},
		{"4000", "fund-general", 0, 30_000, ""},
	}},
	{"je-0007", "2026-09-24", "Arduino kits, Adafruit (2027-0002)", "purchase_request", "req-0002", []Line{
		{"5100", "fund-buildspace", 18_640, 0, "program"},
		{"1000", "fund-buildspace", 0, 18_640, ""},
	}},
	{"je-0009", "2026-09-24", "Release Buildspace restriction for Arduino kits", "release", "req-0002", []Line{
		{"3100", "fund-buildspace", 18_640, 0, ""},
		{"3000", "fund-buildspace", 0, 18_640, ""},
	}},
	{"je-0008", "2026-10-01", "BuildHer grant awarded, payment pending", "contribution", "", []Line{
		{"1150", "fund-buildher", 250_000, 0, ""},
		{"4100", "fund-buildher", 0, 250_000, ""},
	}},
}

type Contribution struct {
	ID, Donor, Fund, Date, Kind string
	AmountCents, QuidProQuo     int64
	Entry                       string
}

var Contributions = []Contribution{
	{"contrib-0001", "donor-foundation", "fund-buildspace", "2026-08-15", "check", 500_000, 0, "je-0002"},
	{"contrib-0002", "donor-acme", "fund-general", "2026-09-05", "online", 150_000, 0, "je-0003"},
	{"contrib-0003", "donor-alum", "fund-general", "2026-09-20", "online", 30_000, 0, "je-0006"},
	{"contrib-0004", "donor-city", "fund-buildher", "2026-10-01", "other", 250_000, 0, "je-0008"},
}
