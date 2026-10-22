package v2portrait

import (
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// Receipt-only routers own a scheduler reader and no publication services.
// Confirmed absence can never create a task, even for ordinary POST requests.
func NewRelightReceiptOnlyProjects(db DonorConfirmationTransactions) (*RelightProjectServices,error) {
 if db==nil {return nil,v2input.ErrInvalid}
 return &RelightProjectServices{db:db,services:map[[2]uuid.UUID]*RelightSubmissionService{}},nil
}
func NewRelightAnalysisReceiptOnlyProjects(db DonorConfirmationTransactions) (*RelightAnalysisProjects,error) {
 if db==nil {return nil,v2input.ErrInvalid}
 return &RelightAnalysisProjects{db:db,services:map[analysisProjectKey]*RelightAnalysisService{}},nil
}
