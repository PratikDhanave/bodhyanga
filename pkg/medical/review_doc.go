// review.go — human-in-the-loop clinical review request type and codecs.
//
// Overview:
//   Models a single clinical action awaiting reviewer decision. The
//   human_review agent records ReviewRequest entries; once a reviewer
//   approves, modifies, or rejects, the original action resumes onto the
//   message bus via the recorded NextTo / NextType / Payload fields.
//
// Key declarations:
//   - ReviewStatus: lifecycle constants (pending, approved, modified, rejected, expired)
//   - ReviewRequest: full review record with patient/case context and SLA
//   - EncodeReview / DecodeReview: JSON helpers for bus transport
//
// See doc.go for package-level documentation.

package medical
