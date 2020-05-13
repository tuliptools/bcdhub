package seed

import (
	"github.com/baking-bad/bcdhub/cmd/api/handlers"
	"github.com/baking-bad/bcdhub/internal/config"
	"github.com/baking-bad/bcdhub/internal/database"
)

// Run -
func Run(ctx *handlers.Context, seed config.SeedConfig) error {
	// 1. seed user
	user := database.User{
		Login:     seed.User.Login,
		Name:      seed.User.Name,
		AvatarURL: seed.User.AvatarURL,
	}

	if err := ctx.DB.GetOrCreateUser(&user); err != nil {
		return err
	}

	ctx.OAUTH.UserID = user.ID

	// 2. seed subscriptions
	for _, sub := range seed.Subscriptions {
		subscription := database.Subscription{
			UserID:  user.ID,
			Address: sub.Address,
			Network: sub.Network,
		}

		if err := ctx.DB.UpsertSubscription(&subscription); err != nil {
			return err
		}
	}

	// 3. seed aliases
	for _, a := range seed.Aliases {
		alias := database.Alias{
			Alias:   a.Alias,
			Network: a.Network,
			Address: a.Address,
		}

		if err := ctx.DB.CreateOrUpdateAlias(&alias); err != nil {
			return err
		}
	}

	// 4. seed accounts
	for _, a := range seed.Accounts {
		account := database.Account{
			UserID:        user.ID,
			PrivateKey:    a.PrivateKey,
			PublicKeyHash: a.PublicKeyHash,
			Network:       a.Network,
		}

		if err := ctx.DB.GetOrCreateAccount(&account); err != nil {
			return err
		}
	}
	return nil
}
