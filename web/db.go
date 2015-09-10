package web

import (
	"log"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"github.com/jmoiron/sqlx"
	"errors"
	"github.com/go-sql-driver/mysql"
)

type DB struct {
	*sqlx.DB
}

type AdminCommands interface {
}

type Commands interface {
	LastRun() (*time.Time, error)
	PopularLanguages() []LanguageCount
	PopularDevs() []DevCount
	Language(name string) []*LanguageResult
	Profile(name string) (*ProfileData, error)
	Search(term string) *[]User
}

func (db *DB) LastRun() (*time.Time, error) {
	timeStr := mysql.NullTime{}
	err := db.Get(&timeStr, queryLastRun)
	if !timeStr.Valid {
		err = errors.New("null time in LastRun call results")
		log.Println(err.Error())
		return nil, err
	}
	return &timeStr.Time, err
}

type LanguageCount struct {
	Language string
	Count    int
	Users    int
}

func (db *DB) PopularLanguages() []LanguageCount {
	langs := []LanguageCount{}
	err := db.Select(&langs, queryPopularLanguages)
	if err != nil {
		log.Println(err)
		return nil
	}
	return langs
}

type DevCount struct {
	Login, Name, AvatarUrl, Followers string
	Stars                             int
	Forks                             int
}

func (db *DB) PopularDevs() []DevCount {
	devs := []DevCount{}
	err := db.Select(&devs, queryPopularDevs)
	if err != nil {
		log.Println(err)
		return nil
	}
	return devs
}

type LanguageResult struct {
	Owner string
	Repos []Repository
	Count int
}

func (db *DB) Language(name string) []*LanguageResult {
	repos := []struct {
		Repository
		Count int
	}{}
	err := db.Select(&repos, queryLanguage, name, name)
	if err != nil {
		log.Println(err)
		return nil
	}
	results := []*LanguageResult{}
	var cursor *LanguageResult
	for _, repo := range repos {
		if cursor == nil || cursor.Owner != *repo.Owner {
			cursor = &LanguageResult{Owner: *repo.Owner, Repos: []Repository{repo.Repository}, Count: repo.Count}
			results = append(results, cursor)
		} else {
			cursor.Repos = append(cursor.Repos, repo.Repository)
		}
	}
	return results
}

type ProfileData struct {
	User  *github.User
	Repos map[string][]github.Repository
}

func (db *DB) Profile(name string) (*ProfileData, error) {
	user := &github.User{}
	reposByLang := map[string][]github.Repository{}
	profile := &ProfileData{user, reposByLang}
	err := db.Get(profile.User, queryProfileForUser, name)
	if err != nil {
		log.Println("Error querying profile")
		return nil, err
	}

	if profile.User.Blog != nil && *profile.User.Blog != "" && !strings.HasPrefix(*profile.User.Blog, "http://") {
		*profile.User.Blog = "http://" + *profile.User.Blog
	}

	repos := []github.Repository{}
	err = db.Select(&repos, queryRepoForUser, profile.User.Login)
	if err != nil {
		log.Println("Error querying repo for user", *profile.User.Login)
		return nil, err
	}

	for _, repo := range repos {
		lang := *repo.Language
		if _, ok := reposByLang[lang]; !ok {
			reposByLang[lang] = []github.Repository{repo}
			continue
		}
		reposByLang[lang] = append(reposByLang[lang], repo)
	}

	return profile, nil
}

func (db *DB) Search(term string) *[]User {
	query := "%" + term + "%"
	users := []User{}
	if err := db.Select(&users, querySearch, query, query); err != nil {
		log.Println(err)
		return nil
	}

	return &users
}

const (
	queryLastRun = `
		select created_at
		from agg_meta
		order by created_at desc
		limit 1;`

	queryPopularLanguages = `
		select language, count(*) as count, count(distinct(owner)) as users
		from agg_repo
		where language is not null
		group by language
		order by count desc;`

	queryPopularDevs = `
		select login, name, avatar_url, followers, stars, forks
		from stldevs.agg_user user
		join(
			select owner, sum(stargazers_count) as stars, sum(forks_count) as forks
			from stldevs.agg_repo
			group by owner
		) repo ON (repo.owner=user.login)
		where name is not null and stars > 100
		order by stars desc;`

	queryLanguage = `
		SELECT r1.owner, r1.name, r1.description, r1.forks_count, r1.stargazers_count, r1.watchers_count, r1.fork, count
		FROM agg_repo r1
		JOIN (
			select owner, sum(stargazers_count) as count
			from stldevs.agg_repo
			where language=?
			group by owner
		) r2 ON ( r2.owner = r1.owner )
		where language=?
		order by r2.count desc, r2.owner, stargazers_count desc;`

	queryProfileForUser = `
		select login, email, name, blog, followers, public_repos, public_gists, avatar_url
		from agg_user
		where login=?`

	queryRepoForUser = `
		select name, language, forks_count, stargazers_count
		from agg_repo
		where owner=? and language is not null
		order by language, stargazers_count desc, name`

	querySearch = `
		select *
		from agg_user
		where login like ? or name like ?`
)
