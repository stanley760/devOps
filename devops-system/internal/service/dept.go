// 部门/岗位管理服务。
//
// 部门树内存组装（无 ancestors 列，对齐 yudao）；更新时校验父节点不得是自身或后代，
// 防止把子树挂到自身之下形成环。岗位 code 租户内唯一、创建后不可改。
package service

import (
	"context"

	"devops-infra/errs"

	"devops-system/internal/auth"
	"devops-system/internal/dao"
	"devops-system/internal/model"
	"devops-system/internal/types"
)

// Dept 部门管理服务。
type Dept struct{}

// NewDept 创建服务。
func NewDept() *Dept { return &Dept{} }

// List 部门树（status 筛选可选）。
func (s *Dept) List(ctx context.Context, status *int8) ([]types.DeptTreeResp, error) {
	depts, err := dao.ListDepts(ctx, status)
	if err != nil {
		return nil, err
	}
	return buildDeptTree(depts), nil
}

// Get 部门详情。
func (s *Dept) Get(ctx context.Context, id types.ID) (*types.DeptDTO, error) {
	d, err := dao.GetDeptByID(ctx, id.Int64())
	if err != nil {
		return nil, err
	}
	dto := toDeptDTO(d)
	return &dto, nil
}

// Create 创建部门：校验父部门存在。
func (s *Dept) Create(ctx context.Context, claims *auth.AccessClaims, req types.DeptSaveReq) error {
	if req.ParentID.Int64() > 0 {
		if _, err := dao.GetDeptByID(ctx, req.ParentID.Int64()); err != nil {
			return err
		}
	}
	d := &model.Dept{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Creator: claims.Username, Updater: claims.Username},
		},
		Name:         req.Name,
		ParentID:     req.ParentID.Int64(),
		Sort:         req.Sort,
		LeaderUserID: req.LeaderUserID.Int64(),
		Phone:        req.Phone,
		Email:        req.Email,
		Status:       req.Status,
	}
	return dao.CreateDept(ctx, d)
}

// Update 更新部门：父节点不能是自身或自身后代（防环）。
func (s *Dept) Update(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.DeptSaveReq) error {
	d, err := dao.GetDeptByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	parentID := req.ParentID.Int64()
	if parentID != 0 {
		if parentID == d.ID {
			return errs.New(errs.CodeParam, "父部门不能是自身")
		}
		if _, err := dao.GetDeptByID(ctx, parentID); err != nil {
			return err
		}
		// 后代校验：parentID 落在以 d 为根的子树内则成环
		depts, err := dao.ListDepts(ctx, nil)
		if err != nil {
			return err
		}
		childrenOf := make(map[int64][]int64, len(depts))
		for _, dep := range depts {
			childrenOf[dep.ParentID] = append(childrenOf[dep.ParentID], dep.ID)
		}
		if inSubtree(childrenOf, d.ID, parentID) {
			return errs.New(errs.CodeParam, "父部门不能是自身的子部门")
		}
	}

	return dao.UpdateDept(ctx, d.ID, map[string]any{
		"name":           req.Name,
		"parent_id":      parentID,
		"sort":           req.Sort,
		"leader_user_id": req.LeaderUserID.Int64(),
		"phone":          req.Phone,
		"email":          req.Email,
		"status":         req.Status,
		"updater":        claims.Username,
	})
}

// Delete 删除部门：有子部门/有用户不可删。
func (s *Dept) Delete(ctx context.Context, id types.ID) error {
	deptID := id.Int64()
	if _, err := dao.GetDeptByID(ctx, deptID); err != nil {
		return err
	}
	has, err := dao.DeptHasChildren(ctx, deptID)
	if err != nil {
		return err
	}
	if has {
		return errs.New(errs.CodeParam, "存在子部门，无法删除")
	}
	has, err = dao.DeptHasUsers(ctx, deptID)
	if err != nil {
		return err
	}
	if has {
		return errs.New(errs.CodeParam, "部门下存在用户，无法删除")
	}
	return dao.DeleteDept(ctx, deptID)
}

// inSubtree target 是否在以 root 为根的子树内（BFS）。
func inSubtree(childrenOf map[int64][]int64, root, target int64) bool {
	queue := []int64{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == target {
			return true
		}
		queue = append(queue, childrenOf[cur]...)
	}
	return false
}

// buildDeptTree 部门列表 → 树（parent 不在集合或为 0 即根）。
func buildDeptTree(depts []model.Dept) []types.DeptTreeResp {
	byID := make(map[int64]*types.DeptTreeResp, len(depts))
	roots := make([]*types.DeptTreeResp, 0)
	for i := range depts {
		node := &types.DeptTreeResp{DeptDTO: toDeptDTO(&depts[i])}
		byID[depts[i].ID] = node
	}
	for i := range depts {
		d := &depts[i]
		node := byID[d.ID]
		if parent, ok := byID[d.ParentID]; ok && d.ParentID != d.ID {
			parent.Children = append(parent.Children, *node)
		} else {
			roots = append(roots, node)
		}
	}
	out := make([]types.DeptTreeResp, 0, len(roots))
	for _, n := range roots {
		out = append(out, *n)
	}
	return out
}

// toDeptDTO model.Dept → DTO。
func toDeptDTO(d *model.Dept) types.DeptDTO {
	return types.DeptDTO{
		ID:           types.ID(d.ID),
		Name:         d.Name,
		ParentID:     types.ID(d.ParentID),
		Sort:         d.Sort,
		LeaderUserID: types.ID(d.LeaderUserID),
		Phone:        d.Phone,
		Email:        d.Email,
		Status:       d.Status,
		CreateTime:   d.CreateTime,
	}
}

// ---- 岗位 ----

// Post 岗位管理服务。
type Post struct{}

// NewPost 创建服务。
func NewPost() *Post { return &Post{} }

// List 分页查询。
func (s *Post) List(ctx context.Context, q types.PostPageQuery) ([]types.PostDTO, int64, error) {
	posts, total, err := dao.ListPosts(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]types.PostDTO, 0, len(posts))
	for i := range posts {
		out = append(out, toPostDTO(&posts[i]))
	}
	return out, total, nil
}

// Get 岗位详情。
func (s *Post) Get(ctx context.Context, id types.ID) (*types.PostDTO, error) {
	p, err := dao.GetPostByID(ctx, id.Int64())
	if err != nil {
		return nil, err
	}
	dto := toPostDTO(p)
	return &dto, nil
}

// Create 创建岗位：code 租户内唯一。
func (s *Post) Create(ctx context.Context, claims *auth.AccessClaims, req types.PostSaveReq) error {
	exists, err := dao.PostCodeExists(ctx, req.Code, 0)
	if err != nil {
		return err
	}
	if exists {
		return errs.New(errs.CodeParam, "岗位编码已存在")
	}
	p := &model.Post{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Creator: claims.Username, Updater: claims.Username},
		},
		Code:   req.Code,
		Name:   req.Name,
		Sort:   req.Sort,
		Status: req.Status,
		Remark: req.Remark,
	}
	return dao.CreatePost(ctx, p)
}

// Update 更新岗位（code 创建后不可改）。
func (s *Post) Update(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.PostSaveReq) error {
	p, err := dao.GetPostByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	return dao.UpdatePost(ctx, p.ID, map[string]any{
		"name":    req.Name,
		"sort":    req.Sort,
		"status":  req.Status,
		"remark":  req.Remark,
		"updater": claims.Username,
	})
}

// Delete 删除岗位：被用户 post_ids 引用不可删 → 软删（改写 code 保同名重建）。
func (s *Post) Delete(ctx context.Context, claims *auth.AccessClaims, id types.ID) error {
	p, err := dao.GetPostByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	has, err := dao.PostReferencedByUsers(ctx, p.ID)
	if err != nil {
		return err
	}
	if has {
		return errs.New(errs.CodeParam, "岗位已被用户引用，请先解除关联")
	}
	return dao.SoftDeletePost(ctx, p.ID, p.Code)
}

// toPostDTO model.Post → DTO。
func toPostDTO(p *model.Post) types.PostDTO {
	return types.PostDTO{
		ID:         types.ID(p.ID),
		Code:       p.Code,
		Name:       p.Name,
		Sort:       p.Sort,
		Status:     p.Status,
		Remark:     p.Remark,
		CreateTime: p.CreateTime,
	}
}
