package models

import (
	"errors"
	"strings"
)

const SubscriptionPeriodSeconds = 2592000

type PlanTerms struct {
	PriceStars  int    `json:"price_stars"`
	Description string `json:"description"`
	Benefits    string `json:"benefits"`
	Terms       string `json:"terms"`
}

func (p *PlanTerms) Validate() error {
	p.Description = strings.TrimSpace(p.Description)
	p.Benefits = strings.TrimSpace(p.Benefits)
	p.Terms = strings.TrimSpace(p.Terms)
	if p.PriceStars < 1 || p.PriceStars > 10000 {
		return errors.New("price_stars must be 1-10000")
	}
	if p.Terms == "" || len(p.Terms) > 4000 {
		return errors.New("terms must be 1-4000 characters")
	}
	if len(p.Description) > 2000 || len(p.Benefits) > 2000 {
		return errors.New("description and benefits must be at most 2000 characters")
	}
	return nil
}
