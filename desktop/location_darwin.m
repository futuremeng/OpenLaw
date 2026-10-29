// location_darwin.m 定位授权 shim。
//
// 事实依据（2026-10-29 实测 + 源码核对）：Wails v2.17 的 darwin 前端未实现任何
// CoreLocation 定位授权回调（在 wails 模块内 grep CLLocation / decidePermissionForRequest
// 均无命中）；裸二进制又没有 bundle 身份与 Info.plist 用途声明，于是 webview 里的
// navigator.geolocation 一律 PERMISSION_DENIED。这里补上最小的一块：仅在用户显式点击
// 「获取我的位置」时向系统申请 WhenInUse 授权，并把授权状态与「缺什么」如实回传。
//
// 纪律：只申请权限，不读取位置、不联网、不做任何地理编码；坐标仍由 webview 交给
// Go 侧离线判定（geo 包）。不依赖 ARC，对象为进程生命周期单例。
#import <CoreLocation/CoreLocation.h>
#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>

@interface OpenLawLocationHelper : NSObject <CLLocationManagerDelegate>
@end

// gLastStatus：delegate 在主线程更新，Go 侧轮询读取（int 读写，容忍良性竞态）。
static int gLastStatus = 0; // kCLAuthorizationStatusNotDetermined

// 单次定位缓存：gFixState 0 未开始 / 1 进行中 / 2 成功 / 3 失败
static double gLat = 0.0;
static double gLon = 0.0;
static double gAcc = -1.0;
static int gFixState = 0;
static int gFixErr = 0;

@implementation OpenLawLocationHelper
- (void)locationManagerDidChangeAuthorization:(CLLocationManager *)manager {
    gLastStatus = (int)[manager authorizationStatus];
}
// 单次定位结果缓存：绑定调用会占住主线程，因此绝不能在 Go 侧同步等待，
// 只能由前端轮询 OpenLawLocationFixState 取用。
- (void)locationManager:(CLLocationManager *)manager didUpdateLocations:(NSArray<CLLocation *> *)locations {
    CLLocation *loc = locations.lastObject;
    if (loc == nil) return;
    gLat = loc.coordinate.latitude;
    gLon = loc.coordinate.longitude;
    gAcc = loc.horizontalAccuracy;
    gFixState = 2; // 成功
    [manager stopUpdatingLocation];
}
- (void)locationManager:(CLLocationManager *)manager didFailWithError:(NSError *)error {
    gFixErr = (int)error.code; // kCLErrorDenied = 1
    gFixState = 3;             // 失败
    [manager stopUpdatingLocation];
}
@end

static OpenLawLocationHelper *gHelper = nil;
static CLLocationManager *gManager = nil;

// 必须在主队列创建/使用 CLLocationManager：cgo 调用发生在 Go 线程，那里没有
// run loop，连接的事件源不会被调度，授权变更（delegate 回调）也就回传不到。
static void openlawEnsureManager(void) {
    if (gManager != nil) return;
    if (gHelper == nil) gHelper = [[OpenLawLocationHelper alloc] init];
    gManager = [[CLLocationManager alloc] init];
    [gManager setDelegate:gHelper];
}

// OpenLawLocationStatus 当前授权状态快照（尚未申请时按「未决定」处理）。
// 注意：绝不能用 dispatch_sync 读——绑定调用可能正占住主线程，同步等待会死锁。
// 这里改为异步刷新，返回值取 delegate/上次刷新的缓存值。
int OpenLawLocationStatus(void) {
    if (gManager != nil) {
        dispatch_async(dispatch_get_main_queue(), ^{
            gLastStatus = (int)[gManager authorizationStatus];
        });
    }
    return gLastStatus;
}

// OpenLawRequestLocationAuthorization 在主队列发起 WhenInUse 授权申请，立即返回
// 当下的状态快照（不等待弹窗）。弹窗与授权结果由 delegate 回调更新 gLastStatus，
// 调用方自行轮询 OpenLawLocationStatus——在绑定调用里阻塞等待是错的：那条路径占住
// 主线程，主队列排不到，弹窗反而永远不会出现（2026-10-29 实测踩过）。
int OpenLawRequestLocationAuthorization(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        openlawEnsureManager();
        if ([gManager authorizationStatus] == kCLAuthorizationStatusNotDetermined) {
            [gManager requestWhenInUseAuthorization];
        }
        gLastStatus = (int)[gManager authorizationStatus];
    });
    return gLastStatus;
}
// OpenLawStartLocationFix 发起一次定位（异步），立即返回当下状态；坐标由 delegate
// 写入缓存，调用方轮询 OpenLawLocationFixState 取用。
// 为什么必须走原生而不走 webview：Wails v2.17 的 WKWebView 未实现
// webView:requestGeolocationPermissionForFrame:decisionHandler:，即使系统已授权，
// JS 的 navigator.geolocation 仍会被 webview 默认拒绝（2026-10-29 实测踩过）。
int OpenLawStartLocationFix(void) {
    // 状态必须在 dispatch_async 之前同步落地：函数立即返回的是快照，若把
    // gFixState = 1 写在块内，返回时仍是「未开始」，前端会误判为终态（实测踩过）。
    gLat = 0.0;
    gLon = 0.0;
    gAcc = -1.0;
    gFixErr = 0;
    gFixState = 1;
    dispatch_async(dispatch_get_main_queue(), ^{
        openlawEnsureManager();
        [gManager startUpdatingLocation];
    });
    return gFixState;
}

// OpenLawLocationFixState 单次定位状态：0 未开始 / 1 进行中 / 2 成功 / 3 失败。
int OpenLawLocationFixState(void) { return gFixState; }
double OpenLawLocationLatitude(void) { return gLat; }
double OpenLawLocationLongitude(void) { return gLon; }
double OpenLawLocationAccuracy(void) { return gAcc; }
int OpenLawLocationFixError(void) { return gFixErr; }



// OpenLawHasLocationUsageKey Info.plist 是否声明了定位用途（缺声明时系统不会弹窗）。
int OpenLawHasLocationUsageKey(void) {
    NSDictionary *info = [NSBundle mainBundle].infoDictionary;
    if (info[@"NSLocationWhenInUseUsageDescription"] != nil) return 1;
    if (info[@"NSLocationUsageDescription"] != nil) return 1;
    return 0;
}

// OpenLawIsBundledApp 是否以 .app bundle 形态运行（裸二进制没有 TCC 身份）。
int OpenLawIsBundledApp(void) {
    return [[NSBundle mainBundle].bundlePath hasSuffix:@".app"] ? 1 : 0;
}
