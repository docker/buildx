package pb

func CreateAttestations(attests []*Attest) map[string]*string {
	result := map[string]*string{}
	for _, attest := range attests {
		// ignore duplicates
		if _, ok := result[attest.Type]; ok {
			continue
		}

		if attest.Disabled {
			result[attest.Type] = nil
			continue
		}

		attrs := attest.Attrs
		result[attest.Type] = &attrs
	}
	return result
}
