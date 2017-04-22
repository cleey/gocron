package task

import (
    "gopkg.in/macaron.v1"
    "github.com/ouqiang/gocron/models"
    "github.com/ouqiang/gocron/modules/logger"
    "github.com/ouqiang/gocron/modules/utils"
    "github.com/ouqiang/gocron/service"
    "strconv"
    "github.com/jakecoffman/cron"
)

type TaskForm struct {
    Id int
    Name string `binding:"Required;"`
    Spec string `binding:"Required;MaxSize(64)"`
    Protocol models.TaskProtocol `binding:"In(1,2,3)"`
    Command string `binding:"Required;MaxSize(512)"`
    Timeout int `binding:"Range(0,86400)"`
    RetryTimes int8
    HostId int16
    Remark string
    Status models.Status `binding:"In(1,2)"`
}

func Index(ctx *macaron.Context)  {
    taskModel := new(models.Task)
    queryParams := parseQueryParams(ctx)
    tasks, err := taskModel.List(queryParams)
    if err != nil {
        logger.Error(err)
    }
    setHostsToTemplate(ctx)
    ctx.Data["Params"] = queryParams
    ctx.Data["Title"] = "任务列表"
    ctx.Data["Tasks"] = tasks
    ctx.HTML(200, "task/index")
}

func Create(ctx *macaron.Context)  {
    setHostsToTemplate(ctx)
    ctx.Data["Title"] = "添加任务"
    ctx.HTML(200, "task/task_form")
}

func Edit(ctx *macaron.Context)  {
    id := ctx.ParamsInt(":id")
    hostModel := new(models.Host)
    hosts, err := hostModel.List(models.CommonMap{})
    if err != nil || len(hosts) == 0 {
        logger.Error(err)
    }
    taskModel := new(models.Task)
    task, err := taskModel.Detail(id)
    if err != nil || task.Id != id {
        logger.Errorf("编辑任务#获取任务详情失败#任务ID-%d#%s", id, err.Error())
        ctx.Redirect("/task")
    }
    ctx.Data["Task"]  = task
    ctx.Data["Title"] = "编辑"
    ctx.Data["Hosts"] = hosts
    if len(hosts) > 0 {
        ctx.Data["FirstHostName"] = hosts[0].Name
        ctx.Data["FirstHostId"] = hosts[0].Id
    }
    ctx.HTML(200, "task/task_form")
}

// 保存任务
func Store(ctx *macaron.Context, form TaskForm) string  {
    json := utils.JsonResponse{}
    taskModel := models.Task{}
    var id int = form.Id
    _, err := cron.Parse(form.Spec)
    if err != nil {
        return json.CommonFailure("crontab表达式解析失败", err)
    }
    nameExists, err := taskModel.NameExist(form.Name, form.Id)
    if err != nil {
        return json.CommonFailure(utils.FailureContent, err)
    }
    if nameExists {
        return json.CommonFailure("任务名称已存在")
    }

    if form.Protocol == models.TaskSSH && form.HostId <= 0 {
        return json.CommonFailure("请选择主机名")
    }

    if form.Protocol != models.TaskHTTP {
        taskModel.HostId = form.HostId
    } else {
        taskModel.HostId = 0
    }

    taskModel.Name = form.Name
    taskModel.Protocol = form.Protocol
    taskModel.Command = form.Command
    taskModel.Timeout = form.Timeout
    taskModel.Remark = form.Remark
    taskModel.Status = form.Status
    taskModel.RetryTimes = form.RetryTimes
    if taskModel.Status != models.Enabled {
        taskModel.Status = models.Disabled
    }
    taskModel.Spec = form.Spec
    if id == 0 {
        id, err = taskModel.Create()
    } else {
        _, err = taskModel.UpdateBean(id)
    }
    if err != nil {
        return json.CommonFailure("保存失败", err)
    }

    // 任务处于激活状态,加入调度管理
    if (taskModel.Status == models.Enabled) {
        addTaskToTimer(id)
    }

    return json.Success("保存成功", nil)
}

// 删除任务
func Remove(ctx *macaron.Context) string {
    id  := ctx.ParamsInt(":id")
    json := utils.JsonResponse{}
    taskModel := new(models.Task)
    _, err := taskModel.Delete(id)
    if err != nil {
        return json.CommonFailure(utils.FailureContent, err)
    }

    service.Cron.RemoveJob(strconv.Itoa(id))

    return json.Success(utils.SuccessContent, nil)
}

// 激活任务
func Enable(ctx *macaron.Context) string {
    return changeStatus(ctx, models.Enabled)
}

// 暂停任务
func Disable(ctx *macaron.Context) string {
    return changeStatus(ctx, models.Disabled)
}

// 手动运行任务
func Run(ctx *macaron.Context) string {
    id := ctx.ParamsInt(":id")
    json := utils.JsonResponse{}
    taskModel := new(models.Task)
    task , err := taskModel.Detail(id)
    if err != nil || task.Id <= 0 {
        return json.CommonFailure("获取任务详情失败", err)
    }

    task.Spec = "手动运行"
    serviceTask := new(service.Task)
    serviceTask.Run(task)

    return json.Success("任务已开始运行, 请到任务日志中查看结果", nil);
}

// 改变任务状态
func changeStatus(ctx *macaron.Context, status models.Status) string {
    id  := ctx.ParamsInt(":id")
    json := utils.JsonResponse{}
    taskModel := new(models.Task)
    _, err := taskModel.Update(id, models.CommonMap{
        "Status": status,
    })
    if err != nil {
        return json.CommonFailure(utils.FailureContent, err)
    }

    if status == models.Enabled {
        addTaskToTimer(id)
    } else {
        service.Cron.RemoveJob(strconv.Itoa(id))
    }

    return json.Success(utils.SuccessContent, nil)
}

// 添加任务到定时器
func addTaskToTimer(id int)  {
    taskModel := new(models.Task)
    task, err := taskModel.Detail(id)
    if err != nil {
        logger.Error(err)
        return
    }

    taskService := service.Task{}
    taskService.Add(task)
}

// 解析查询参数
func parseQueryParams(ctx *macaron.Context) (models.CommonMap) {
    var params models.CommonMap = models.CommonMap{}
    params["Id"] = ctx.QueryInt("id")
    params["HostId"] = ctx.QueryInt("host_id")
    params["Name"] = ctx.QueryTrim("name")
    params["Protocol"] = ctx.QueryInt("protocol")
    params["Status"] = ctx.QueryInt("status") - 1
    params["Page"] = ctx.QueryInt("page")
    params["PageSize"] = ctx.QueryInt("page_size")

    return params
}

func setHostsToTemplate(ctx *macaron.Context)  {
    hostModel := new(models.Host)
    hosts, err := hostModel.List(models.CommonMap{})
    if err != nil || len(hosts) == 0 {
        logger.Error(err)
    }
    ctx.Data["Hosts"] = hosts
    if len(hosts) > 0 {
        ctx.Data["FirstHostName"] = hosts[0].Name
        ctx.Data["FirstHostId"] = hosts[0].Id
    }
}