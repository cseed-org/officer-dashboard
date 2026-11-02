package store

import "context"

// DesignStages is the board order.
var DesignStages = []string{"new", "accepted", "in-progress", "in-review", "delivered"}

// DesignTypes are the request types offered by the form.
var DesignTypes = []string{"poster", "social", "slides", "merch", "video", "other"}

// DesignRequest is a design_requests row.
type DesignRequest struct {
	ID          string
	Title       string
	Type        string
	Formats     string
	DueDate     *string
	Urgency     string
	Brief       string
	Refs        string
	EventID     *string
	Program     *string
	Stage       string
	RequesterID string
	AssigneeID  *string
	ApprovedBy  *string
	ApprovedAt  *int64
	CreatedAt   int64
	UpdatedAt   int64
}

const designCols = `id, title, type, formats, due_date, urgency, brief, refs, event_id, program, stage, requester_id, assignee_id, approved_by, approved_at, created_at, updated_at`

func scanDesign(s interface{ Scan(...any) error }) (*DesignRequest, error) {
	var d DesignRequest
	if err := s.Scan(&d.ID, &d.Title, &d.Type, &d.Formats, &d.DueDate, &d.Urgency, &d.Brief, &d.Refs, &d.EventID, &d.Program, &d.Stage,
		&d.RequesterID, &d.AssigneeID, &d.ApprovedBy, &d.ApprovedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// DesignRequests returns every request, most recently updated first.
func (q *Queries) DesignRequests(ctx context.Context) ([]DesignRequest, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+designCols+` FROM design_requests ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DesignRequest{}
	for rows.Next() {
		d, err := scanDesign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// DesignRequestByID returns ErrNotFound for an unknown id.
func (q *Queries) DesignRequestByID(ctx context.Context, id string) (*DesignRequest, error) {
	d, err := scanDesign(q.q.QueryRowContext(ctx, `SELECT `+designCols+` FROM design_requests WHERE id = ?`, id))
	return d, notFound(err)
}

func (q *Queries) InsertDesignRequest(ctx context.Context, d DesignRequest) error {
	return q.Exec(ctx, `INSERT INTO design_requests(`+designCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Title, d.Type, d.Formats, d.DueDate, d.Urgency, d.Brief, d.Refs, d.EventID, d.Program, d.Stage,
		d.RequesterID, d.AssigneeID, d.ApprovedBy, d.ApprovedAt, d.CreatedAt, d.UpdatedAt)
}

// SaveDesignRequest writes every mutable column of d.
func (q *Queries) SaveDesignRequest(ctx context.Context, d DesignRequest) error {
	return q.Exec(ctx, `UPDATE design_requests SET title=?, type=?, formats=?, due_date=?, urgency=?, brief=?, refs=?, event_id=?, program=?,
 stage=?, assignee_id=?, approved_by=?, approved_at=?, updated_at=? WHERE id=?`,
		d.Title, d.Type, d.Formats, d.DueDate, d.Urgency, d.Brief, d.Refs, d.EventID, d.Program, d.Stage, d.AssigneeID, d.ApprovedBy, d.ApprovedAt, d.UpdatedAt, d.ID)
}

func (q *Queries) DeleteDesignRequest(ctx context.Context, id string) error {
	return q.Exec(ctx, `DELETE FROM design_requests WHERE id=?`, id)
}

// DesignCounts returns comment and file counts per request.
func (q *Queries) DesignCounts(ctx context.Context) (comments, files map[string]int, err error) {
	comments, files = map[string]int{}, map[string]int{}
	for _, t := range []struct {
		table string
		into  map[string]int
	}{{"design_request_comments", comments}, {"design_request_files", files}} {
		rows, err := q.q.QueryContext(ctx, `SELECT request_id, count(*) FROM `+t.table+` GROUP BY request_id`)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return nil, nil, err
			}
			t.into[id] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
	}
	return comments, files, nil
}

// DesignComment is a design_request_comments row.
type DesignComment struct {
	ID        string
	RequestID string
	AuthorID  string
	Body      string
	CreatedAt int64
}

func (q *Queries) InsertDesignComment(ctx context.Context, c DesignComment) error {
	return q.Exec(ctx, `INSERT INTO design_request_comments(id,request_id,author_id,body,created_at) VALUES(?,?,?,?,?)`, c.ID, c.RequestID, c.AuthorID, c.Body, c.CreatedAt)
}

func (q *Queries) DesignComments(ctx context.Context, requestID string) ([]DesignComment, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id,request_id,author_id,body,created_at FROM design_request_comments WHERE request_id=? ORDER BY created_at, id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DesignComment{}
	for rows.Next() {
		var c DesignComment
		if err := rows.Scan(&c.ID, &c.RequestID, &c.AuthorID, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DesignFile is a design_request_files row.
type DesignFile struct {
	ID          string
	RequestID   string
	Kind        string
	Filename    string
	MimeType    string
	SizeBytes   int64
	Storage     string
	StorageKey  string
	WebViewLink *string
	UploadedBy  string
	CreatedAt   int64
}

const designFileCols = `id,request_id,kind,filename,mime_type,size_bytes,storage,storage_key,web_view_link,uploaded_by,created_at`

func scanDesignFile(s interface{ Scan(...any) error }) (*DesignFile, error) {
	var f DesignFile
	if err := s.Scan(&f.ID, &f.RequestID, &f.Kind, &f.Filename, &f.MimeType, &f.SizeBytes, &f.Storage, &f.StorageKey, &f.WebViewLink, &f.UploadedBy, &f.CreatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}

func (q *Queries) InsertDesignFile(ctx context.Context, f DesignFile) error {
	return q.Exec(ctx, `INSERT INTO design_request_files(`+designFileCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		f.ID, f.RequestID, f.Kind, f.Filename, f.MimeType, f.SizeBytes, f.Storage, f.StorageKey, f.WebViewLink, f.UploadedBy, f.CreatedAt)
}

func (q *Queries) DesignFiles(ctx context.Context, requestID string) ([]DesignFile, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+designFileCols+` FROM design_request_files WHERE request_id=? ORDER BY created_at, id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DesignFile{}
	for rows.Next() {
		f, err := scanDesignFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (q *Queries) DesignFileByID(ctx context.Context, id string) (*DesignFile, error) {
	f, err := scanDesignFile(q.q.QueryRowContext(ctx, `SELECT `+designFileCols+` FROM design_request_files WHERE id=?`, id))
	return f, notFound(err)
}

// DesignTeamIDs returns the ids of active members of the subteam named "Design".
func (q *Queries) DesignTeamIDs(ctx context.Context) ([]string, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT u.id FROM users u
 JOIN officer_profiles p ON p.user_id = u.id JOIN subteams s ON s.id = p.subteam_id
 WHERE u.active = 1 AND s.name = 'Design' COLLATE NOCASE ORDER BY u.name, u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// IsDesigner reports whether the user is an active member of the Design subteam.
func (q *Queries) IsDesigner(ctx context.Context, userID string) (bool, error) {
	var n int
	err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM users u JOIN officer_profiles p ON p.user_id = u.id JOIN subteams s ON s.id = p.subteam_id
 WHERE u.id = ? AND u.active = 1 AND s.name = 'Design' COLLATE NOCASE`, userID).Scan(&n)
	return n > 0, err
}
