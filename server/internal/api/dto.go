package api

import (
	"encoding/json"
	"time"

	"officerdashboard/internal/accounting"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/store"
)

// JSON shapes of src/core/contracts.ts. Field order follows the contract; nullable fields
// are pointers so null (not absent) goes on the wire.

type UserSummary struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
}

type CurrentUser struct {
	UserSummary
	IsOwner   bool    `json:"isOwner"`
	DiscordID string  `json:"discordId"`
	Role      string  `json:"role"`
	Title     *string `json:"title"`
}

type AccountOption struct {
	ID          string  `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Form990Line *string `json:"form990Line"`
}

type EventCounts struct {
	Registered           int      `json:"registered"`
	CheckedIn            int      `json:"checkedIn"`
	Reflections          int      `json:"reflections"`
	MatchedReflections   int      `json:"matchedReflections"`
	UnmatchedReflections int      `json:"unmatchedReflections"`
	CheckInRate          *float64 `json:"checkInRate"`
	ReflectionRate       *float64 `json:"reflectionRate"`
}

type EventListItem struct {
	ID          string        `json:"id"`
	LumaID      *string       `json:"lumaId"`
	Name        string        `json:"name"`
	StartsAt    string        `json:"startsAt"`
	EndsAt      *string       `json:"endsAt"`
	Location    *string       `json:"location"`
	Host        *UserSummary  `json:"host"`
	HostSubteam *SubteamBrief `json:"hostSubteam"`
	Status      string        `json:"status"`
	Capacity    *int64        `json:"capacity"`
	LumaURL     *string       `json:"lumaUrl"`
	CoverURL    *string       `json:"coverUrl"`
	Metrics     EventCounts   `json:"metrics"`
}

type SubteamBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type NotionLink struct {
	URL *string `json:"url"`
	// Placeholder is true while the NotionSync stub is in use (no page exists).
	Placeholder bool `json:"placeholder"`
}

type EventDetail struct {
	EventListItem
	URL             *string    `json:"url"`
	Description     *string    `json:"description"`
	Notion          NotionLink `json:"notion"`
	DesignRequestID *string    `json:"designRequestId"`
	UpdatedAt       string     `json:"updatedAt"`
}

type AccountBrief = accounting.AccountBrief
type FundBrief = accounting.FundBrief

type RequestListItem struct {
	ID             string       `json:"id"`
	Number         string       `json:"number"`
	Title          string       `json:"title"`
	Vendor         string       `json:"vendor"`
	Type           string       `json:"type"`
	Status         string       `json:"status"`
	TotalCents     int64        `json:"totalCents"`
	Requester      UserSummary  `json:"requester"`
	ExpenseAccount AccountBrief `json:"expenseAccount"`
	Fund           FundBrief    `json:"fund"`
	EventID        *string      `json:"eventId"`
	SubmittedAt    *string      `json:"submittedAt"`
	UpdatedAt      string       `json:"updatedAt"`
}

type ApprovalDTO struct {
	ID        string      `json:"id"`
	Director  UserSummary `json:"director"`
	Decision  string      `json:"decision"`
	Comment   *string     `json:"comment"`
	CreatedAt string      `json:"createdAt"`
}

type AttachmentDTO struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Filename   string      `json:"filename"`
	MimeType   string      `json:"mimeType"`
	SizeBytes  int64       `json:"sizeBytes"`
	Storage    string      `json:"storage"`
	URL        *string     `json:"url"`
	UploadedBy UserSummary `json:"uploadedBy"`
	CreatedAt  string      `json:"createdAt"`
}

type SettlementHold struct {
	AttemptedCents int64  `json:"attemptedCents"`
	ApprovedCents  int64  `json:"approvedCents"`
	Reason         string `json:"reason"`
	HeldAt         string `json:"heldAt"`
}

type EventRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RequestCore is a request detail without the per-viewer fields; agent jobs embed it.
type RequestCore struct {
	RequestListItem
	ItemURL          *string         `json:"itemUrl"`
	Quantity         int64           `json:"quantity"`
	UnitCostCents    int64           `json:"unitCostCents"`
	TaxShipCents     int64           `json:"taxShipCents"`
	ActualTotalCents *int64          `json:"actualTotalCents"`
	FunctionalClass  string          `json:"functionalClass"`
	Justification    string          `json:"justification"`
	NeededBy         *string         `json:"neededBy"`
	Event            *EventRef       `json:"event"`
	DecidedAt        *string         `json:"decidedAt"`
	CreatedAt        string          `json:"createdAt"`
	Approvals        []ApprovalDTO   `json:"approvals"`
	Attachments      []AttachmentDTO `json:"attachments"`
	JournalEntryIDs  []string        `json:"journalEntryIds"`
	SettlementHold   *SettlementHold `json:"settlementHold"`
}

// RequestDetail adds what the current viewer may do.
type RequestDetail struct {
	RequestCore
	AllowedTransitions []string `json:"allowedTransitions"`
	CanEdit            bool     `json:"canEdit"`
}

type FulfillmentJob struct {
	ID        string      `json:"id"`
	Status    string      `json:"status"`
	Attempts  int         `json:"attempts"`
	Request   RequestCore `json:"request"`
	CreatedAt string      `json:"createdAt"`
	ClaimedAt *string     `json:"claimedAt"`
}

type AdminUser struct {
	CurrentUser
	Email       *string `json:"email"`
	Active      bool    `json:"active"`
	LastLoginAt *string `json:"lastLoginAt"`
	CreatedAt   string  `json:"createdAt"`
}

type ServiceTokenDTO struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Prefix     string      `json:"prefix"`
	Scopes     []string    `json:"scopes"`
	CreatedBy  UserSummary `json:"createdBy"`
	CreatedAt  string      `json:"createdAt"`
	LastUsedAt *string     `json:"lastUsedAt"`
	ExpiresAt  *string     `json:"expiresAt"`
	RevokedAt  *string     `json:"revokedAt"`
}

type page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// ─── helpers ───

// isoMs renders epoch milliseconds like JS toISOString.
func isoMs(ms int64) string { return luma.ISO(time.UnixMilli(ms)) }

func isoPtr(ms *int64) *string {
	if ms == nil {
		return nil
	}
	s := isoMs(*ms)
	return &s
}

func strPtr(s string) *string { return &s }

func userSummary(u *store.User) UserSummary {
	return UserSummary{ID: u.ID, Name: u.Name, AvatarURL: u.AvatarURL}
}

func currentUser(u *store.User) CurrentUser {
	return CurrentUser{UserSummary: userSummary(u), DiscordID: u.DiscordID, Role: u.Role, IsOwner: u.IsOwner, Title: u.Title}
}

func adminUser(u *store.User) AdminUser {
	return AdminUser{CurrentUser: currentUser(u), Email: u.Email, Active: u.Active, LastLoginAt: isoPtr(u.LastLoginAt), CreatedAt: isoMs(int64(u.CreatedAt))}
}

func accountOption(a accounting.Account) AccountOption {
	return AccountOption{ID: a.ID, Code: a.Code, Name: a.Name, Type: a.Type, Form990Line: a.Form990Line}
}

func ratio(num, den int) *float64 {
	if den <= 0 {
		return nil
	}
	v := float64(num) / float64(den)
	return &v
}

func eventCounts(c *store.Counts) EventCounts {
	var z store.Counts
	if c == nil {
		c = &z
	}
	return EventCounts{
		Registered: c.Registered, CheckedIn: c.CheckedIn, Reflections: c.Reflect, MatchedReflections: c.Matched,
		UnmatchedReflections: c.Reflect - c.Matched,
		CheckInRate:          ratio(c.CheckedIn, c.Registered),
		ReflectionRate:       ratio(c.Matched, c.CheckedIn),
	}
}

// snapshot turns a value into the JSON stored in audit before/after images.
func snapshot(v any) *string {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	s := string(raw)
	return &s
}
