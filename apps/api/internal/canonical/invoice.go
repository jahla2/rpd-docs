package canonical

type Evidence struct {
    Page int `json:"page"`
    BlockIDs []string `json:"block_ids,omitempty"`
    BBox []float64 `json:"bbox,omitempty"`
    Method string `json:"method"`
    Confidence float64 `json:"confidence"`
}

type Field struct {
    Raw string `json:"raw"`
    Value string `json:"value"`
    Evidence Evidence `json:"evidence"`
}

type InvoiceLine struct {
    Description string `json:"description"`
    ItemCode string `json:"item_code,omitempty"`
    Quantity string `json:"quantity,omitempty"`
    Unit string `json:"unit,omitempty"`
    UnitPrice string `json:"unit_price,omitempty"`
    Tax string `json:"tax,omitempty"`
    LineTotal string `json:"line_total,omitempty"`
    CalculatedLineTotal string `json:"calculated_line_total,omitempty"`
}

type Invoice struct {
    SchemaVersion string `json:"schema_version"`
    DocumentID string `json:"document_id"`
    SupplierName Field `json:"supplier_name"`
    SupplierCode *Field `json:"supplier_code,omitempty"`
    InvoiceNumber Field `json:"invoice_number"`
    InvoiceDate Field `json:"invoice_date"`
    DueDate *Field `json:"due_date,omitempty"`
    PONumber *Field `json:"po_number,omitempty"`
    Currency Field `json:"currency"`
    Subtotal *Field `json:"subtotal,omitempty"`
    Tax *Field `json:"tax,omitempty"`
    GrandTotal Field `json:"grand_total"`
    LineItems []InvoiceLine `json:"line_items"`
    RequiresReview bool `json:"requires_review"`
}
