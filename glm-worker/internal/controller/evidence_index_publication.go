package controller

import "fmt"

func (s *Store) validatePublicationIndexAuthority(record TransitionRecord, input EvidencePublicationInput) error {
	for _, ref := range input.TaskRevisionRefs {
		revision, err := s.LoadTaskIndexRevision(ref)
		if err != nil {
			return err
		}
		if revision.ControllerGeneration != record.CommittedGeneration || revision.TransitionID != record.TransitionID {
			return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "task index revision is bound to different transition authority"}
		}
	}
	for _, ref := range input.EpisodeRevisionRefs {
		revision, err := s.LoadEpisodeIndexRevision(ref)
		if err != nil {
			return err
		}
		if revision.ControllerGeneration != record.CommittedGeneration || revision.TransitionID != record.TransitionID {
			return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "episode index revision is bound to different transition authority"}
		}
	}
	return nil
}

func (s *Store) validateEpisodeTaskHeadPublication(publishedTaskHeads []EvidenceSubjectHead, episodeRefs []EvidenceObjectRef) error {
	published, err := publishedTaskEvidenceHeads(publishedTaskHeads)
	if err != nil {
		return err
	}
	for _, episodeRef := range episodeRefs {
		if err := s.validateEpisodeTaskHeadsAgainstPublished(published, episodeRef); err != nil {
			return err
		}
	}
	return nil
}

func publishedTaskEvidenceHeads(heads []EvidenceSubjectHead) (map[string]EvidenceObjectRef, error) {
	published := make(map[string]EvidenceObjectRef, len(heads))
	for _, head := range heads {
		if _, exists := published[head.SubjectID]; exists {
			return nil, &EvidenceIntegrityError{Digest: head.RevisionRef.Digest, Reason: "published task evidence head contains duplicate subject"}
		}
		published[head.SubjectID] = head.RevisionRef
	}
	return published, nil
}

func (s *Store) validateEpisodeTaskHeadsAgainstPublished(published map[string]EvidenceObjectRef, episodeRef EvidenceObjectRef) error {
	episode, err := s.LoadEpisodeIndexRevision(episodeRef)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(episode.TaskIndexHeads))
	for _, taskHead := range episode.TaskIndexHeads {
		if seen[taskHead.SubjectID] {
			return &EvidenceIntegrityError{Digest: episodeRef.Digest, Reason: "episode task index heads contain duplicate subject"}
		}
		seen[taskHead.SubjectID] = true
		if err := s.validateEpisodeTaskHeadAgainstPublished(published, taskHead); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateEpisodeTaskHeadAgainstPublished(published map[string]EvidenceObjectRef, taskHead EvidenceSubjectHead) error {
	publishedRef, ok := published[taskHead.SubjectID]
	if !ok {
		return &EvidenceIntegrityError{Digest: taskHead.RevisionRef.Digest, Reason: "episode task index head is not published task authority"}
	}
	return s.validatePublishedTaskRevision(taskHead.SubjectID, publishedRef, taskHead.RevisionRef)
}

func (s *Store) validatePublishedTaskRevision(subject string, publishedRef, targetRef EvidenceObjectRef) error {
	current := publishedRef
	seen := map[string]bool{}
	for {
		key := evidenceRefKey(current)
		if seen[key] {
			return &EvidenceIntegrityError{Digest: current.Digest, Reason: "published task index chain contains a cycle"}
		}
		seen[key] = true
		if evidenceRefsEqual(current, targetRef) {
			return nil
		}
		revision, err := s.LoadTaskIndexRevision(current)
		if err != nil {
			return err
		}
		if taskEvidenceSubjectID(revision.TaskRef) != subject {
			return &EvidenceIntegrityError{Digest: current.Digest, Reason: "published task index chain subject is inconsistent"}
		}
		if revision.PreviousRevision == nil {
			return &EvidenceIntegrityError{Digest: targetRef.Digest, Reason: fmt.Sprintf("episode task index head for subject %s is not reachable from published task authority", subject)}
		}
		current = *revision.PreviousRevision
	}
}
