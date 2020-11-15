package poc

import (
	"fmt"
	"github.com/google/cel-go/cel"
	"github.com/zsdevX/DarkEye/common"
	"github.com/zsdevX/DarkEye/hack/poc/xraypoc"
	"github.com/zsdevX/DarkEye/hack/poc/xraypoc/celtypes"
	"regexp"
	"sort"
	"strings"
)

func (p *Poc) doCheck(pocFileName, myUrl string) (bool, error) {
	poc, err := xraypoc.LoadPoc(pocFileName)
	if err != nil {
		return false, err
	}
	newLib := xraypoc.NewCustomLib()
	params, err := p.loadParamsPrepare(newLib, myUrl, poc)
	if err != nil {
		return false, err
	}
	env, err := cel.NewEnv(cel.Lib(newLib))
	if err != nil {
		return false, err
	}
	err = loadParams(poc, params, env)
	if err != nil {
		return false, err
	}
	//执行规则
	result := false
	for _, rule := range poc.Rules {
		for k1, v1 := range params {
			v := fmt.Sprintf("%v", v1)
			for k2, v2 := range rule.Headers {
				rule.Headers[k2] = strings.ReplaceAll(v2, "{{"+k1+"}}", v)
			}
			rule.Path = strings.ReplaceAll(strings.TrimSpace(rule.Path), "{{"+k1+"}}", v)
			rule.Body = strings.ReplaceAll(strings.TrimSpace(rule.Body), "{{"+k1+"}}", v)
		}
		response, err := tryReq(myUrl, &rule)
		if err != nil {
			return result, err
		}
		params["response"] = response
		//匹配rule.Search
		if rule.Search != "" {
			searchResult := trySearch(rule.Search, response.Body)
			if searchResult == nil {
				return false, err
			}
			//匹配结果作为下一个rule的替换依据
			for k, v := range searchResult {
				params[k] = v
			}
		}
		//匹配rule.Expression
		out, err := xraypoc.Eval(env, rule.Expression, params)
		if err != nil {
			return result, err
		}
		if fmt.Sprintf("%v", out) == "false" {
			result = false
			break
		}
		result = true
	}
	return result, nil
}

func (p *Poc) loadParamsPrepare(newLib *xraypoc.CustomLib, myUrl string, poc *xraypoc.Poc) (params map[string]interface{}, err error) {
	params = make(map[string]interface{})
	for k, v := range poc.Set {
		newLib.UpdateOptions(k, xraypoc.ConvertDeclType(v))
		//预处理
		if v == "newReverse()" {
			params[k], err = p.newReverse()
			if err != nil {
				return nil, err
			}
		}
		if strings.HasPrefix(v, "request.") {
			//Fixme: 在rule中发送get/post时，是否需要增加path，例如refrer=request.url+rule.path
			url, err := xraypoc.StringConvertUrl(myUrl)
			if err != nil {
				return nil, err
			}
			params["request"] = xraypoc_celtypes.Request{
				Url: url,
			}
		}
	}
	return
}

func loadParams(poc *xraypoc.Poc, params map[string]interface{}, env *cel.Env) error {
	keys := make([]string, 0)
	for k := range poc.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	//按顺序处理 params
	for _, k := range keys {
		v := poc.Set[k]
		/*Note
		*'newReverse'预处理中处理化
		*'Payload'通常为Set最后的变量，需等到其它变量初始化完成
		*/
		if v == "newReverse()" ||
			k == "payload" {
			continue
		}
		result, err := xraypoc.Eval(env, v, params)
		if err != nil {
			return err
		}
		switch val := result.Value().(type) {
		case *xraypoc_celtypes.UrlType:
			params[k] = xraypoc.UrlConvertString(val)
		default:
			params[k] = result
		}
	}
	//结束 params
	for k, v := range poc.Set {
		if v != "payload" {
			continue
		}
		out, err := xraypoc.Eval(env, v, params)
		if err != nil {
			return err
		}
		params[k] = out
	}
	return nil
}

func trySearch(re string, body []byte) map[string]string {
	r, err := regexp.Compile(re)
	if err != nil {
		return nil
	}
	result := r.FindSubmatch(body)
	names := r.SubexpNames()
	if len(result) > 1 && len(names) > 1 {
		params := make(map[string]string)
		for i, name := range names {
			if i > 0 && i <= len(result) {
				params[name] = string(result[i])
			}
		}
		return params
	}
	return nil
}

func (p *Poc) newReverse() (*xraypoc_celtypes.Reverse, error) {
	url, err := xraypoc.StringConvertUrl(p.ReverseUrl)
	if err != nil {
		return nil, err
	}
	return &xraypoc_celtypes.Reverse{
		Url: url,
	}, nil
}

func tryReq(myUrl string, rule *xraypoc.Rules) (xraypoc_celtypes.Response, error) {
	req := common.HttpRequest{
		Method:           rule.Method,
		Url:              myUrl + rule.Path,
		Body:             []byte(rule.Body),
		Headers:          rule.Headers,
		NoFollowRedirect: !rule.FollowRedirects,
		TimeOut:          10,
	}
	response, err := req.Go()
	if err != nil {
		return xraypoc_celtypes.Response{}, err
	}
	return xraypoc_celtypes.Response{
		Body:    response.Body,
		Status:  response.Status,
		Headers: response.ResponseHeaders,
		ContentType:response.ContentType,
	}, err
}
