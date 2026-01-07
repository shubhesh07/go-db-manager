package main

import (
	"github.com/shubhesh07/go-db-manager/entity"
)

// User represents a user entity
type User struct {
	entity.BaseEntity
	Username  string   `orm:"column:username;unique;not_null" json:"username"`
	Email     string   `orm:"column:email;unique;not_null" json:"email"`
	Password  string   `orm:"column:password;not_null" json:"-"`
	FirstName string   `orm:"column:first_name" json:"first_name"`
	LastName  string   `orm:"column:last_name" json:"last_name"`
	IsActive  bool     `orm:"column:is_active;default:true" json:"is_active"`
	Profile   *Profile `orm:"one_to_one;foreign_key:user_id" json:"profile,omitempty"`
	Posts     []*Post  `orm:"one_to_many;foreign_key:user_id" json:"posts,omitempty"`
}

// TableName returns the table name for User
func (u *User) TableName() string {
	return "users"
}

// Profile represents a user profile
type Profile struct {
	entity.BaseEntity
	UserID    uint   `orm:"column:user_id;not_null;index" json:"user_id"`
	Bio       string `orm:"column:bio;type:text" json:"bio"`
	AvatarURL string `orm:"column:avatar_url" json:"avatar_url"`
	User      *User  `orm:"many_to_one;foreign_key:user_id;references:id" json:"user,omitempty"`
}

// TableName returns the table name for Profile
func (p *Profile) TableName() string {
	return "profiles"
}

// Post represents a blog post
type Post struct {
	entity.BaseEntity
	UserID    uint   `orm:"column:user_id;not_null;index" json:"user_id"`
	Title     string `orm:"column:title;not_null" json:"title"`
	Content   string `orm:"column:content;type:text" json:"content"`
	Published bool   `orm:"column:published;default:false" json:"published"`
	User      *User  `orm:"many_to_one;foreign_key:user_id;references:id" json:"user,omitempty"`
	Tags      []*Tag `orm:"many_to_many;join_table:post_tags;join_column:post_id;inverse_column:tag_id" json:"tags,omitempty"`
}

// TableName returns the table name for Post
func (p *Post) TableName() string {
	return "posts"
}

// Tag represents a post tag
type Tag struct {
	entity.BaseEntity
	Name  string  `orm:"column:name;unique;not_null" json:"name"`
	Posts []*Post `orm:"many_to_many;join_table:post_tags;join_column:tag_id;inverse_column:post_id" json:"posts,omitempty"`
}

// TableName returns the table name for Tag
func (t *Tag) TableName() string {
	return "tags"
}
