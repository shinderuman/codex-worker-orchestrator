package harnesslint

func scanQualitySurface(root string, paths []string) ([]Violation, error) {
	wiring, err := qualityWiringViolations(root, paths)
	if err != nil {
		return nil, err
	}
	streams, err := processStreamViolations(root, paths)
	if err != nil {
		return nil, err
	}
	return append(wiring, streams...), nil
}

